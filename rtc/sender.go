package rtc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mengelbart/syncodec"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/gcc/pkg/gcc"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const initialTargetBitrate = 800_000

func senderSignalCandidate(addr string, c *webrtc.ICECandidate) error {
	payload := []byte(c.ToJSON().Candidate)
	resp, err := http.Post(fmt.Sprintf("http://%s/candidate", addr), "application/json; charset=utf-8", bytes.NewReader(payload)) //nolint:noctx
	if err != nil {
		return err
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		return closeErr
	}

	return nil
}

func StartSender(offerAddr, answerAddr string) error {
	var candidatesMux sync.Mutex
	pendingCandidates := make([]*webrtc.ICECandidate, 0)

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	mediaEngine := &webrtc.MediaEngine{}
	err := mediaEngine.RegisterDefaultCodecs()
	if err != nil {
		return err
	}

	sw := &sampleWriter{}
	encoder, err := syncodec.NewStatisticalEncoder(
		sw,
		syncodec.WithInitialTargetBitrate(initialTargetBitrate),
	)
	if err != nil {
		return err
	}

	registry := interceptor.Registry{}

	if err = registerRTPSenderDumper(&registry); err != nil {
		return err
	}
	if err = registerGCC(&registry, gccLoopFactory(encoder)); err != nil {
		return err
	}

	if err = webrtc.ConfigureTWCCHeaderExtensionSender(mediaEngine, &registry); err != nil {
		return err
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(&registry),
	).NewPeerConnection(config)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	trackLocalStaticSample, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		return err
	}
	rtpSender, err := peerConnection.AddTrack(trackLocalStaticSample)
	if err != nil {
		return err
	}

	sw.setTrack(trackLocalStaticSample)

	// When an ICE candidate is available send to the other Pion instance
	// the other Pion instance will add this candidate by calling AddICECandidate
	peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		candidatesMux.Lock()
		defer candidatesMux.Unlock()

		desc := peerConnection.RemoteDescription()
		if desc == nil {
			pendingCandidates = append(pendingCandidates, c)
		} else if onICECandidateErr := senderSignalCandidate(answerAddr, c); onICECandidateErr != nil {
			panic(onICECandidateErr)
		}
	})

	// A HTTP handler that allows the other Pion instance to send us ICE candidates
	// This allows us to add ICE candidates faster, we don't have to wait for STUN or TURN
	// candidates which may be slower
	http.HandleFunc("/candidate", func(_ http.ResponseWriter, r *http.Request) {
		candidate, candidateErr := ioutil.ReadAll(r.Body)
		if candidateErr != nil {
			panic(candidateErr)
		}
		if candidateErr := peerConnection.AddICECandidate(webrtc.ICECandidateInit{Candidate: string(candidate)}); candidateErr != nil {
			panic(candidateErr)
		}
	})

	// A HTTP handler that processes a SessionDescription given to us from the other Pion process
	http.HandleFunc("/sdp", func(_ http.ResponseWriter, r *http.Request) {
		sdp := webrtc.SessionDescription{}
		if sdpErr := json.NewDecoder(r.Body).Decode(&sdp); sdpErr != nil {
			panic(sdpErr)
		}

		if sdpErr := peerConnection.SetRemoteDescription(sdp); sdpErr != nil {
			panic(sdpErr)
		}

		candidatesMux.Lock()
		defer candidatesMux.Unlock()

		for _, c := range pendingCandidates {
			if onICECandidateErr := senderSignalCandidate(answerAddr, c); onICECandidateErr != nil {
				panic(onICECandidateErr)
			}
		}
	})
	// Start HTTP server that accepts requests from the answer process
	go func() { panic(http.ListenAndServe(offerAddr, nil)) }()

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
			fmt.Println("Peer Connection has gone to failed exiting")
			os.Exit(0)
		}
	})

	// Create an offer to send to the other process
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		return err
	}

	// Sets the LocalDescription, and starts our UDP listeners
	// Note: this will start the gathering of ICE candidates
	if err = peerConnection.SetLocalDescription(offer); err != nil {
		return err
	}

	// Send our offer to the HTTP server listening in the other process
	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://%s/sdp", answerAddr), "application/json; charset=utf-8", bytes.NewReader(payload)) // nolint:noctx
	if err != nil {
		return err
	}

	if err = resp.Body.Close(); err != nil {
		return err
	}

	go func() {
		for {
			if _, _, err = rtpSender.ReadRTCP(); err != nil {
				if errors.Is(io.EOF, err) {
					fmt.Printf("rtpSender.ReadRTCP got EOF\n")
					return
				}
				fmt.Printf("rtpSender.ReadRTCP returned error: %v\n", err)
				return
			}
		}

	}()

	go encoder.Start()

	//	go func() {
	//		ticker := time.NewTicker(20 * time.Millisecond)
	//		for now := range ticker.C {
	//			target, err := bwe.GetTargetBitrate("")
	//			if err != nil {
	//				// TODO
	//				panic(err)
	//			}
	//			stats, err := bwe.GetStats("")
	//			if err != nil {
	//				// TODO
	//				panic(err)
	//			}
	//			lossEstimate := 0
	//			delayEstimate := 0
	//			estimate := 0.0
	//			thresh := 0.0
	//			rtt := time.Duration(0)
	//			if stats != nil {
	//				lossEstimate = stats.LossBasedEstimate
	//				delayEstimate = stats.Bitrate
	//				estimate = stats.Estimate
	//				thresh = stats.Threshold
	//				rtt = stats.RTT
	//			}
	//			fmt.Fprintf(ccWriter, "%v, %v, %v, %v, %v, %v, %v\n", now.UnixMilli(), target, lossEstimate, delayEstimate, estimate, thresh, rtt.Milliseconds())
	//			encoder.SetTargetBitrate(target)
	//		}
	//	}()

	select {}
}

type sampleWriter struct {
	track *webrtc.TrackLocalStaticSample
}

func (w *sampleWriter) setTrack(track *webrtc.TrackLocalStaticSample) {
	w.track = track
}

func (w *sampleWriter) WriteFrame(frame syncodec.Frame) {
	w.track.WriteSample(media.Sample{
		Data:               frame.Content,
		Timestamp:          time.Time{},
		Duration:           frame.Duration,
		PacketTimestamp:    0,
		PrevDroppedPackets: 0,
	})
}

func gccLoopFactory(encoder *syncodec.StatisticalCodec) gcc.NewPeerConnectionCallback {
	return func(_ string, bwe gcc.BandwidthEstimator) {
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			for range ticker.C {
				target := bwe.GetTargetBitrate()
				if target < 0 {
					log.Printf("got negative target bitrate: %v\n", target)
					continue
				}
				encoder.SetTargetBitrate(target)
				fmt.Printf("new bitrate: %v\n", target)
			}
		}()
	}
}

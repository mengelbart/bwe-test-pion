package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mengelbart/syncodec"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func getLogWriters() (rtp, rtcp, cc io.Writer) {
	var err error
	rtp, err = os.Create("log/rtp_out.log")
	check(err)
	rtcp, err = os.Create("log/rtcp_in.log")
	check(err)
	cc, err = os.Create("log/cc.log")
	check(err)
	return
}

func signalCandidate(addr string, c *webrtc.ICECandidate) error {
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

func main() { //nolint:gocognit
	offerAddr := flag.String("offer-address", ":50000", "Address that the Offer HTTP server is hosted on.")
	answerAddr := flag.String("answer-address", "localhost:60000", "Address that the Answer HTTP server is hosted on.")
	flag.Parse()

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
	check(err)

	rtpWriter, rtcpWriter, ccWriter := getLogWriters()

	rtpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(rtpWriter),
	)

	check(err)
	rtcpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcpWriter),
	)
	check(err)

	interceptorRegistry := &interceptor.Registry{}
	interceptorRegistry.Add(rtpDumperInterceptor)
	interceptorRegistry.Add(rtcpDumperInterceptor)

	bwe := gcc.NewSendSideBandwidthEstimator(150_000)
	gcc, err := cc.NewControllerInterceptor(cc.SetBWE(func() cc.BandwidthEstimator { return bwe }))
	check(err)
	interceptorRegistry.Add(gcc)

	check(webrtc.ConfigureTWCCHeaderExtensionSender(mediaEngine, interceptorRegistry))

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
	).NewPeerConnection(config)
	check(err)
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	trackLocalStaticSample, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	check(err)
	rtpSender, err := peerConnection.AddTrack(trackLocalStaticSample)
	check(err)

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
		} else if onICECandidateErr := signalCandidate(*answerAddr, c); onICECandidateErr != nil {
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
			if onICECandidateErr := signalCandidate(*answerAddr, c); onICECandidateErr != nil {
				panic(onICECandidateErr)
			}
		}
	})
	// Start HTTP server that accepts requests from the answer process
	go func() { panic(http.ListenAndServe(*offerAddr, nil)) }()

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
		panic(err)
	}

	// Sets the LocalDescription, and starts our UDP listeners
	// Note: this will start the gathering of ICE candidates
	if err = peerConnection.SetLocalDescription(offer); err != nil {
		panic(err)
	}

	// Send our offer to the HTTP server listening in the other process
	payload, err := json.Marshal(offer)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(fmt.Sprintf("http://%s/sdp", *answerAddr), "application/json; charset=utf-8", bytes.NewReader(payload)) // nolint:noctx

	check(err)
	check(resp.Body.Close())

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

	sw := &sampleWriter{
		track: trackLocalStaticSample,
	}
	encoder, err := syncodec.NewStatisticalEncoder(
		sw,
		syncodec.WithInitialTargetBitrate(150_000),
	)
	check(err)
	go encoder.Start()

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		for now := range ticker.C {
			target := bwe.GetBandwidthEstimation()
			fmt.Printf("new bwe := %v bps\n", target)
			fmt.Fprintf(ccWriter, "%v, %v\n", now.UnixMilli(), target)
			encoder.SetTargetBitrate(int(target))
		}
	}()

	select {}
}

type sampleWriter struct {
	track *webrtc.TrackLocalStaticSample
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

func rtpFormat(pkt *rtp.Packet, attributes interceptor.Attributes) string {
	// TODO(mathis): Replace timestamp by attributes.GetTimestamp as soon as
	// implemented in interceptors

	var twcc rtp.TransportCCExtension
	ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
	check(twcc.Unmarshal(ext))

	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
		twcc.TransportSequence,
	)
}

func rtcpFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	// TODO(mathis): Replace timestamp by attributes.GetTimestamp as soon as
	// implemented in interceptors
	res := fmt.Sprintf("%v\t", time.Now().UnixMilli())
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.TransportLayerCC:
			res += feedback.String()
		}
	}
	return res
}

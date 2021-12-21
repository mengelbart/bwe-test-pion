package rtc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/sdp/v2"
	"github.com/pion/webrtc/v3"
)

func receiverSignalCandidate(addr string, c *webrtc.ICECandidate) error {
	payload := []byte(c.ToJSON().Candidate)
	resp, err := http.Post(fmt.Sprintf("http://%s/candidate", addr), // nolint:noctx
		"application/json; charset=utf-8", bytes.NewReader(payload))
	if err != nil {
		return err
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		return closeErr
	}

	return nil
}

func onTrack(trackRemote *webrtc.TrackRemote, rtpReceiver *webrtc.RTPReceiver) {
	//ts := time.Now()
	for {
		if err := rtpReceiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("failed to SetReadDeadline for rtpReceiver: %v\n", err)
		}
		if err := trackRemote.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("failed to SetReadDeadline for trackRemote: %v", err)
		}

		_, _, err := trackRemote.ReadRTP()
		if err == io.EOF {
			fmt.Printf("trackRemote.ReadRTP received EOF\n")
			return
		}
		//fmt.Printf("%v\t since last arrival: %v\n", time.Since(ts), p.Header)
		//ts = time.Now()
		if err != nil {
			fmt.Printf("trackRemote.ReadRTP returned error: %v\n", err)
			return
		}
	}
}

func StartReceiver(answerAddr, offerAddr string) error {
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

	registry := &interceptor.Registry{}

	mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeVideo)
	if err = mediaEngine.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.TransportCCURI}, webrtc.RTPCodecTypeVideo); err != nil {
		return err
	}

	mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBTransportCC}, webrtc.RTPCodecTypeAudio)
	if err = mediaEngine.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.TransportCCURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return err
	}

	rtpWriter, err := getLogWriter("log/rtp_in.log")
	if err != nil {
		return err
	}
	defer rtpWriter.Close()
	rtcpWriter, err := getLogWriter("log/rtcp_out.log")
	if err != nil {
		return err
	}
	defer rtcpWriter.Close()
	if err = registerRTPReceiverDumper(registry, rtpWriter, rtcpWriter); err != nil {
		return err
	}
	if err = registerTWCC(registry); err != nil {
		return err
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	).NewPeerConnection(config)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := peerConnection.Close(); err != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

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
		} else if onICECandidateErr := receiverSignalCandidate(offerAddr, c); onICECandidateErr != nil {
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
		if hErr := json.NewDecoder(r.Body).Decode(&sdp); hErr != nil {
			panic(hErr)
		}

		if hErr := peerConnection.SetRemoteDescription(sdp); hErr != nil {
			panic(hErr)
		}

		// Create an answer to send to the other process
		answer, hErr := peerConnection.CreateAnswer(nil)
		if hErr != nil {
			panic(hErr)
		}

		// Send our answer to the HTTP server listening in the other process
		payload, hErr := json.Marshal(answer)
		if hErr != nil {
			panic(hErr)
		}
		resp, hErr := http.Post(fmt.Sprintf("http://%s/sdp", offerAddr), "application/json; charset=utf-8", bytes.NewReader(payload)) // nolint:noctx
		if hErr != nil {
			panic(hErr)
		} else if closeErr := resp.Body.Close(); closeErr != nil {
			panic(closeErr)
		}

		// Sets the LocalDescription, and starts our UDP listeners
		err = peerConnection.SetLocalDescription(answer)
		if err != nil {
			panic(err)
		}

		candidatesMux.Lock()
		for _, c := range pendingCandidates {
			onICECandidateErr := receiverSignalCandidate(offerAddr, c)
			if onICECandidateErr != nil {
				panic(onICECandidateErr)
			}
		}
		candidatesMux.Unlock()
	})

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

	peerConnection.OnTrack(onTrack)

	// Start HTTP server that accepts requests from the offer process to exchange SDP and Candidates
	panic(http.ListenAndServe(answerAddr, nil))
}

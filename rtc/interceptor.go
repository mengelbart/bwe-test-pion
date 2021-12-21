package rtc

import (
	"io"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/gcc/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/twcc"
)

func registerRTPSenderDumper(r *interceptor.Registry, rtp, rtcp io.Writer) error {
	rtpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(rtp),
	)
	if err != nil {
		return err
	}

	rtcpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcp),
	)
	if err != nil {
		return err
	}
	r.Add(rtpDumperInterceptor)
	r.Add(rtcpDumperInterceptor)
	return nil
}

func registerRTPReceiverDumper(r *interceptor.Registry, rtp, rtcp io.Writer) error {
	rtcpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcp),
	)
	if err != nil {
		return err
	}

	rtpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(rtp),
	)
	if err != nil {
		return err
	}
	r.Add(rtcpDumperInterceptor)
	r.Add(rtpDumperInterceptor)
	return nil
}

func registerTWCC(r *interceptor.Registry) error {
	fbFactory, err := twcc.NewSenderInterceptor()
	if err != nil {
		return err
	}
	r.Add(fbFactory)
	return nil
}

func registerTWCCHeaderExtension(r *interceptor.Registry) error {
	headerExtension, err := twcc.NewHeaderExtensionInterceptor()
	if err != nil {
		return err
	}
	r.Add(headerExtension)
	return nil
}

func registerGCC(r *interceptor.Registry, cb gcc.NewPeerConnectionCallback) error {
	gccFactory, err := gcc.NewInterceptor(gcc.InitialBitrate(initialTargetBitrate), gcc.SetPacer(gcc.NewLeakyBucketPacer(initialTargetBitrate)))
	if err != nil {
		return err
	}
	gccFactory.OnNewPeerConnection(cb)
	r.Add(gccFactory)
	return nil
}

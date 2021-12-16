# Pion test app for BWE

This is a simple test application that sends fake video data from one pion
instance to another. It is a modified version of the [pion-to-pion
example](https://github.com/pion/webrtc/tree/master/examples/pion-to-pion).

Instead of sending messages on datachannels, this app sends RTP packets using a
[fake encoder](https://github.com/mengelbart/syncodec). The encoder will be
configured to output data at the rate calculated by the congestion control
interceptor.

The SDP offer and answer are exchanged automatically over HTTP.
The `answer` side acts like a HTTP server and should therefore be ran first.

## Run locally:

```shell
go run main.go receive
```

and then in a second shell:

```shell
go run main.go send
```

You should see bandwidth estimates (bwe) being updated and increasing.

## Run in docker:

If you want to see the congestion controller in action with a simulated setup,
look at the [bwe-test-runner](https://github.com/mengelbart/bwe-test-runner).


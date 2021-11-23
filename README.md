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
go run answer/main.go
```

and then in a second shell:

```shell
go run offer/main.go
```

You should see bandwidth estimates (bwe) being updated and increasing.

## Run in docker:

Simply run `docker-compose up` and you should see the same output.


At some point you may see the bitrate reaching a maximum bitrate or some errors
occur due to very high target bitrates. This happens, because there is no packet
loss, which is currently the only way of detecting congestion.

If you want to see the congestion controller in action with a simulated setup,
look at the [bwe-test-runner](https://github.com/mengelbart/bwe-test-runner). In
the future, there will also be a similar test runner using only go tests.


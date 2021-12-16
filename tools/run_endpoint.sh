#!/bin/bash

# Set up the routing needed for the simulation.
/setup.sh

set -x

if [ "$ROLE" == "sender" ]; then
    echo "Starting RTP over QUIC sender..."
    QUIC_GO_LOG_LEVEL=error ./bwe-test-pion send -a $RECEIVER:60000 $ARGS
else
    echo "Running RTP over QUIC receiver."
    QUIC_GO_LOG_LEVEL=error ./bwe-test-pion receive -o $SENDER:50000 $ARGS
fi

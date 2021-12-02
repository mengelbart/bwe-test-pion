#!/bin/bash

./setup.sh

#export PION_LOG_INFO=gcc_loss_controller,pacer,cc_interceptor
/offer --answer-address $RECEIVER:60000


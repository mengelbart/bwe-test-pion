package cmd

import (
	"log"

	"github.com/mengelbart/bwe-test-pion/rtc"
	"github.com/spf13/cobra"
)

var (
	senderOfferAddr, senderAnswerAddr string
)

func init() {
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVarP(&senderOfferAddr, "offer", "o", ":50000", "Offer address")
	sendCmd.Flags().StringVarP(&senderAnswerAddr, "answer", "a", "localhost:60000", "Answer address")
}

var sendCmd = &cobra.Command{
	Use: "send",
	Run: func(_ *cobra.Command, _ []string) {
		if err := startSender(); err != nil {
			log.Fatal(err)
		}
	},
}

func startSender() error {
	return rtc.StartSender(senderOfferAddr, senderAnswerAddr)
}

package cmd

import (
	"log"

	"github.com/mengelbart/bwe-test-pion/rtc"
	"github.com/spf13/cobra"
)

var (
	receiverOfferAddr, receiverAnswerAddr string
)

func init() {
	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVarP(&receiverOfferAddr, "offer", "o", "localhost:50000", "Offer address")
	receiveCmd.Flags().StringVarP(&receiverAnswerAddr, "answer", "a", ":60000", "Answer address")
}

var receiveCmd = &cobra.Command{
	Use: "receive",
	Run: func(_ *cobra.Command, _ []string) {
		if err := startReceiver(); err != nil {
			log.Fatal(err)
		}
	},
}

func startReceiver() error {
	return rtc.StartReceiver(receiverAnswerAddr, receiverOfferAddr)
}

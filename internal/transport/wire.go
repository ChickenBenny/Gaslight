package transport

import (
	"github.com/ChickenBenny/Gaslight/internal/chain"
	"github.com/ChickenBenny/Gaslight/internal/rpc"
)

const subscriptionMethod = "eth_subscription"

type subscriptionParams struct {
	Subscription string `json:"subscription"`
	Result       any    `json:"result"`
}

type subscriptionNotification struct {
	JSONRPC string             `json:"jsonrpc"`
	Method  string             `json:"method"`
	Params  subscriptionParams `json:"params"`
}

func newHeadNotification(subID string, head *chain.Block) subscriptionNotification {
	return subscriptionNotification{
		JSONRPC: "2.0",
		Method:  subscriptionMethod,
		Params: subscriptionParams{
			Subscription: subID,
			Result:       rpc.NewHeadResult(head),
		},
	}
}

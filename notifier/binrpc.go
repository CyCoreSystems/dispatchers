package notifier

import (
	"github.com/CyCoreSystems/dispatchers/v2/sets"
	"github.com/CyCoreSystems/go-kamailio/binrpc"
)

// BinRPCNotifier is a dispatchers.Notifier which tells Kamailio to reload its dispatcher module using the binrpc protocol.
type BinRPCNotifier struct{

	// Host is the Kamailio hostname or IP address.
	Host string

	// Port is the UDP port on which Kamailio is listening for binrpc.
	Port string
}

// Notify implements dispatchers.Notifier
func (b *BinRPCNotifier) Notify(sets []*sets.State) error {
	return binrpc.InvokeMethod("dispatcher.reload", b.Host, b.Port)
}

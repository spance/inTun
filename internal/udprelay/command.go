package udprelay

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net"
)

func RunCommand(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "udp" {
		return fmt.Errorf("usage: intun relay udp <target|listen> --address-token <token>")
	}
	if len(args) < 2 || (args[1] != "target" && args[1] != "listen") {
		return fmt.Errorf("usage: intun relay udp <target|listen> --address-token <token>")
	}
	mode := args[1]
	modeArgs := args[2:]
	if mode == "listen" {
		return runListenCommand(ctx, modeArgs, input, output, stderr)
	}
	return runTargetCommand(ctx, modeArgs, input, output, stderr)
}

func runTargetCommand(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) error {
	flags := flag.NewFlagSet("relay udp target", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addressToken := flags.String("address-token", "", "encoded UDP target")
	if err := flags.Parse(args); err != nil {
		return err
	}
	target, err := decodeAddressToken(*addressToken)
	if err != nil {
		return err
	}
	return ServeTargetStream(ctx, input, output, target, RelayOptions{})
}

func runListenCommand(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) error {
	flags := flag.NewFlagSet("relay udp listen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addressToken := flags.String("address-token", "", "encoded UDP listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	listenAddr, err := decodeAddressToken(*addressToken)
	if err != nil {
		return err
	}
	resolved, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("resolve UDP listen address: %w", err)
	}
	listener, err := net.ListenUDP("udp", resolved)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}
	transport := NewStreamTransport(input, output, streamClosers(input, output)...)
	if err := transport.WriteFrame(Frame{Type: FrameReady, Payload: []byte(listener.LocalAddr().String())}); err != nil {
		_ = listener.Close()
		_ = transport.Close()
		return fmt.Errorf("write relay ready frame: %w", err)
	}
	return NewPeerRelay(listener, transport, RelayOptions{}, PeerHooks{}).Serve(ctx)
}

func decodeAddressToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("missing --address-token")
	}
	address, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("invalid address token: %w", err)
	}
	if len(address) == 0 {
		return "", fmt.Errorf("empty address token")
	}
	return string(address), nil
}

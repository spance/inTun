package platform

import "fmt"

type forwarder interface {
	Start(conn *SSHConnection, spec ForwardSpec) error
}

type tcpForwarder struct {
	executor *SSHExecutor
}

func (f tcpForwarder) Start(conn *SSHConnection, spec ForwardSpec) error {
	switch spec.Type {
	case Local:
		return f.executor.startLocalForward(conn, spec.LocalAddr, spec.RemoteAddr)
	case Remote:
		return f.executor.startRemoteForward(conn, spec.LocalAddr, spec.RemoteAddr)
	case Dynamic:
		return f.executor.startDynamicForward(conn, spec.LocalAddr)
	default:
		return fmt.Errorf("UNKNOWN_TUNNEL_TYPE: %s", spec.Type)
	}
}

func (e *SSHExecutor) startForward(conn *SSHConnection, spec ForwardSpec) error {
	forwarder, err := e.forwarderFor(spec)
	if err != nil {
		return err
	}
	return forwarder.Start(conn, spec)
}

func (e *SSHExecutor) forwarderFor(spec ForwardSpec) (forwarder, error) {
	switch spec.Protocol {
	case TCP:
		return tcpForwarder{executor: e}, nil
	case UDP:
		if spec.Type == Dynamic {
			return nil, fmt.Errorf("UNSUPPORTED_FORWARD: %s over UDP", spec.Type)
		}
		return udpForwarder{}, nil
	default:
		return nil, fmt.Errorf("UNKNOWN_NETWORK_PROTOCOL: %s", spec.Protocol)
	}
}

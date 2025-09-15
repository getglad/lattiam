package protocol

const (
	// MaxGRPCRecvMsgSize is the maximum size for gRPC messages we can receive.
	// The default is 4MB which is too small for some provider schemas (e.g., AWS provider).
	// We set this to 256MB to handle even the largest provider schemas.
	MaxGRPCRecvMsgSize = 256 * 1024 * 1024 // 256MB

	// UnixNetwork is the network type for Unix domain sockets
	UnixNetwork = "unix"
)

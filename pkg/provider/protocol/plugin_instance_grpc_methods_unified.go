package protocol

// GetGRPCClientV5 returns the V5 gRPC client if this is a V5 provider
func (p *PluginProviderInstance) GetGRPCClientV5() interface{} {
	return p.grpcClientV5
}

// GetGRPCClientV6 returns the V6 gRPC client if this is a V6 provider
func (p *PluginProviderInstance) GetGRPCClientV6() interface{} {
	return p.grpcClientV6
}

// Package testconfig supplies the historical reporter test oracle only.
// It is never imported by the collector executable or by the build agent.
package testconfig

type AgentConfig struct {
	ProjectId, AgentId, SecretKey, BuildType, Gateway string
}
type AgentEnv struct {
	HostName, CPUProductInfo, GPUProductInfo string
	ip                                       string
}

func (e *AgentEnv) GetAgentIp() string   { return e.ip }
func (e *AgentEnv) SetAgentIp(ip string) { e.ip = ip }

var GAgentConfig *AgentConfig
var GAgentEnv *AgentEnv

func (a *AgentConfig) GetAuthHeaderMap() map[string]string {
	return map[string]string{
		"X-DEVOPS-BUILD-TYPE":       a.BuildType,
		"X-DEVOPS-PROJECT-ID":       a.ProjectId,
		"X-DEVOPS-AGENT-ID":         a.AgentId,
		"X-DEVOPS-AGENT-SECRET-KEY": a.SecretKey,
	}
}

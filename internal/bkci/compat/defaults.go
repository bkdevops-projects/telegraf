//go:build !loong64

package monitor

// platformExtraInputsFn 由平台专属文件通过 init() 注入，返回该平台需要
// 额外添加的 Input（例如 Linux 的 NetProto 读 /proc/net/snmp 产出
// net,interface=all 汇总）。非该平台为 nil，跳过。
var platformExtraInputsFn func() []Input

// newDefaultInputs 返回默认 input 列表。顺序对齐 telegrafConf.go 中
// [[inputs.*]] 出现的顺序，便于 debug 时肉眼对照。
func newDefaultInputs() []Input {
	ins := []Input{
		NewCPU(),
		NewDisk(),
		NewDiskIO(),
		NewMem(),
		NewNet(),
		NewSystem(),
		NewNetstat(),
		NewSwap(),
		NewKernel(),
		NewProcesses(),
	}
	if platformExtraInputsFn != nil {
		ins = append(ins, platformExtraInputsFn()...)
	}
	return ins
}

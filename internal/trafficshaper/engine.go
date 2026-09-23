package trafficshaper

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Engine manages the root TC qdisc hierarchy on the target network interface.
type Engine struct {
	mu       sync.RWMutex
	iface    string
	executor CommandExecutor
}

// NewEngine creates an Engine using the default SysExecutor and detected interface.
func NewEngine(iface string) *Engine {
	if iface == "" {
		iface = detectDefaultInterface()
	}
	return NewEngineWithExecutor(iface, NewSysExecutor())
}

// NewEngineWithExecutor creates an Engine with a custom CommandExecutor.
func NewEngineWithExecutor(iface string, executor CommandExecutor) *Engine {
	if iface == "" {
		iface = detectDefaultInterface()
	}
	if executor == nil {
		executor = NewSysExecutor()
	}
	return &Engine{
		iface:    iface,
		executor: executor,
	}
}

// Interface returns the bound network interface name.
func (e *Engine) Interface() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.iface
}

// Executor returns the underlying CommandExecutor.
func (e *Engine) Executor() CommandExecutor {
	return e.executor
}

// Execute delegates command execution directly to the configured executor.
func (e *Engine) Execute(ctx context.Context, cmd string, args ...string) error {
	return e.executor.Execute(ctx, cmd, args...)
}

// IsSupported checks whether traffic shaping is supported on the host platform.
func (e *Engine) IsSupported() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("tc")
	return err == nil
}

// Init initializes the root HTB qdisc, 1:1 parent class, and 1:9999 unthrottled class.
func (e *Engine) Init(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear any preexisting root qdisc to ensure clean slate; ignore error if not present.
	_ = e.executor.Execute(ctx, "tc", "qdisc", "del", "dev", e.iface, "root")

	if err := e.executor.Execute(ctx, "tc", "qdisc", "replace", "dev", e.iface, "root", "handle", DefaultRootHandle, "htb", "default", "9999"); err != nil {
		return err
	}
	if err := e.executor.Execute(ctx, "tc", "class", "replace", "dev", e.iface, "parent", DefaultRootHandle, "classid", DefaultRootClassID, "htb", "rate", DefaultBandwidth, "ceil", DefaultBandwidth); err != nil {
		return err
	}
	if err := e.executor.Execute(ctx, "tc", "class", "replace", "dev", e.iface, "parent", DefaultRootHandle, "classid", DefaultDirectClassID, "htb", "rate", DefaultBandwidth, "ceil", DefaultBandwidth); err != nil {
		return err
	}
	return nil
}

// Teardown deletes the root qdisc on the network interface.
func (e *Engine) Teardown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	err := e.executor.Execute(ctx, "tc", "qdisc", "del", "dev", e.iface, "root")
	if err != nil {
		// Tolerate missing root qdisc so teardown is safe to call idempotently.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such file or directory") ||
			strings.Contains(msg, "cannot find specified qdisc") ||
			strings.Contains(msg, "cannot delete qdisc with handle of zero") {
			return nil
		}
		return err
	}
	return nil
}

// detectDefaultInterface finds the default outbound network interface.
func detectDefaultInterface() string {
	if iface, err := getInterfaceFromRoute(); err == nil && iface != "" {
		return iface
	}
	if iface, err := getFirstActiveInterface(); err == nil && iface != "" {
		return iface
	}
	return "eth0"
}

// getInterfaceFromRoute parses /proc/net/route to find the default route device.
func getInterfaceFromRoute() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", errors.New("empty route file")
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == "00000000" {
			return fields[0], nil
		}
	}
	return "", errors.New("default route not found")
}

// getFirstActiveInterface returns the first non-loopback up interface with an address.
func getFirstActiveInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err == nil && len(addrs) > 0 {
			return iface.Name, nil
		}
	}
	return "", errors.New("no active interface found")
}

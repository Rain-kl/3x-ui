package panelai

import "os/exec"

func isolateProcess(cmd *exec.Cmd)    {}
func killProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }

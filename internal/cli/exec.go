package cli

import "os/exec"

// execCommand wraps exec.Command for wake-gateway execution.
var execCommand = exec.Command

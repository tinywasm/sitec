package sitec

import (
	"bytes"
	"os"
	"os/exec"

	"github.com/tinywasm/fmt"
)

type execToolchain struct{}

func NewExecToolchain() Toolchain {
	return &execToolchain{}
}

func (t *execToolchain) List(dir string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"list"}, args...)
	cmd := exec.Command("go", fullArgs...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Err(stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (t *execToolchain) ListEnv(dir string, env []string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"list"}, args...)
	cmd := exec.Command("go", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Err(stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (t *execToolchain) Run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Err(stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

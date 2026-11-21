package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestFlowStartupCorrelation exercises the actual exporter entry point with a synthetic API.
func TestFlowStartupCorrelation(t *testing.T) {
	if os.Getenv("FLOW_STARTUP_HELPER") == "1" {
		os.Args = append([]string{os.Args[0]}, strings.Split(os.Getenv("FLOW_STARTUP_ARGS"), "\n")...)
		main()
		return
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()
	for _, tc := range []struct {
		name    string
		netflow bool
	}{{"zenarmor-only", false}, {"netflow-enabled", true}} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--opnsense.protocol=http", "--opnsense.address=" + strings.TrimPrefix(api.URL, "http://"),
				"--opnsense.api-key=synthetic", "--opnsense.api-secret=synthetic", "--log.format=json",
				"--flow.enabled", "--flow.correlate", "--flow.log-mode=per_flow", "--logs.enabled", "--logs.sink=stdout",
				"--logs.zenarmor.enabled", "--logs.zenarmor.listen-http=localhost:0", "--web.listen-address=localhost:0"}
			if tc.netflow {
				args = append(args, "--flow.netflow.enabled", "--flow.netflow.listen=localhost:0")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFlowStartupCorrelation$")
			cmd.Env = []string{"FLOW_STARTUP_HELPER=1", "FLOW_STARTUP_ARGS=" + strings.Join(args, "\n"), "HOME=" + t.TempDir()}
			pipe, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			s := bufio.NewScanner(pipe)
			var output strings.Builder
			for s.Scan() {
				line := s.Text()
				output.WriteString(line + "\n")
				var event map[string]any
				if json.Unmarshal([]byte(line), &event) != nil || event["msg"] != "flow log emission enabled" {
					continue
				}
				if event["correlate"] != tc.netflow {
					t.Errorf("effective correlate = %v, want %v: %s", event["correlate"], tc.netflow, line)
				}
				if !tc.netflow && event["correlate_requested"] != true {
					t.Errorf("requested correlation not distinguished: %s", line)
				}
				return
			}
			t.Fatalf("startup log missing (context: %v, scan: %v):\n%s", ctx.Err(), s.Err(), output.String())
		})
	}
}

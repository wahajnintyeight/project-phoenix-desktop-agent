package main

import (
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestRelaunchEnvironmentDropsDevServer(t *testing.T) {
	got := relaunchEnv([]string{"APPDATA=test", "frontend_devserver_url=http://localhost", "WAILS_INTERNAL_DEV_READY_TOKEN=secret", "WAILS_INTERNAL_DEV_READY_URL=http://localhost", "PHOENIX_TEST_PROFILE=isolated"})
	want := []string{"APPDATA=test", "PHOENIX_TEST_PROFILE=isolated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestWaitForProcess(t *testing.T) {
	// Hold a process open until the test releases it, without relying on a sleep duration.
	cmd := exec.Command("cmd.exe", "/c", "set /p x=")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- waitForProcess(uint32(cmd.Process.Pid)) }()
	select {
	case err := <-done:
		t.Fatalf("returned while parent is running: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	_, _ = stdin.Write([]byte("done\r\n"))
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not observe process exit")
	}
}

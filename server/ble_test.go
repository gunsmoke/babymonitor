package main

import "testing"

func TestParseBLECommandWithEspruinoPrefix(t *testing.T) {
	cmd, ok := parseBLECommand(">\x13\x11{\"t\":\"cmd\",\"cmd\":\"stop\"}")
	if !ok || cmd.Command != "stop" {
		t.Fatalf("parseBLECommand() = %+v, %t; want stop command", cmd, ok)
	}
}

func TestParseBLECommandRejectsNonCommand(t *testing.T) {
	if _, ok := parseBLECommand("console: {\"t\":\"data\",\"cmd\":\"stop\"}"); ok {
		t.Fatal("parseBLECommand accepted a non-command message")
	}
}

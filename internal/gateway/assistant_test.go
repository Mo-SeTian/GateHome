package gateway

import "testing"

func TestAssistantEndpointCredentialScope(t *testing.T) {
	previous := assistantEndpoint{Name: "local", Base: "https://example.test/v1", Model: "model", Key: "TEST_ONLY_KEY"}
	input := assistantSettings{assistantEndpoint: previous}
	input.Key = ""
	input.Base += "/"
	result, err := input.endpoint(previous)
	if err != nil || result.Key != previous.Key {
		t.Fatal("same normalized endpoint should retain the saved credential")
	}
	input.Base = "https://another.example.test/v1"
	result, err = input.endpoint(previous)
	if err != nil || result.Key != "" {
		t.Fatal("a different endpoint must never inherit the credential")
	}
	input.Base = previous.Base
	input.ClearKey = true
	result, err = input.endpoint(previous)
	if err != nil || result.Key != "" {
		t.Fatal("clearing a saved credential should work without resubmitting it")
	}
	input.Base = "https://example.test/v1?token=TEST_ONLY"
	if _, err := input.endpoint(previous); err == nil {
		t.Fatal("endpoint with query parameters should be rejected")
	}
}

func TestAssistantProposalScopesAndValidatesChanges(t *testing.T) {
	config := DefaultConfig()
	config.Routes = []Route{{ID: "album", GroupID: "default", Host: "album.example.test", Upstream: "http://[::1]:2283", Enabled: true}}
	state := State{Config: config, Revision: 7}
	reply := assistantReply{Action: "route_port", Target: "album", Value: "2284"}
	change := assistantProposal(state, reply)
	if change == nil || change.Revision != 7 || change.Before != "http://[::1]:2283" || change.After != "http://[::1]:2284" {
		t.Fatalf("wrong proposal: %#v", change)
	}
	if state.Config.Routes[0].Upstream != change.Before {
		t.Fatal("a proposal must not mutate the current configuration")
	}
	reply.Value = "65536"
	if assistantProposal(state, reply) != nil {
		t.Fatal("an invalid port should not produce a proposal")
	}
	reply.Target, reply.Value = "missing", "2284"
	if assistantProposal(state, reply) != nil {
		t.Fatal("an unknown target should not produce a proposal")
	}
	change = assistantProposal(state, assistantReply{Action: "health_interval", Value: "90"})
	if change == nil || change.Before != "60" || change.After != "90" {
		t.Fatal("older configurations should preview their effective default interval")
	}
}

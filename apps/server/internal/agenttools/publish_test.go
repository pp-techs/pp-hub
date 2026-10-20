package agenttools

import "testing"

// publish_artifact is a public link that runs scripts: it is offered only
// when the instance offers publishing to agents.
func TestPublishToolsAreGated(t *testing.T) {
	has := func(o Options, name string) bool {
		for _, tool := range Tools(o) {
			if tool.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"publish_artifact", "unpublish_artifact"} {
		if has(Options{PublicLinks: true}, name) {
			t.Errorf("%s offered without Publishing", name)
		}
		if !has(Options{PublicLinks: true, Publishing: true}, name) {
			t.Errorf("%s missing with Publishing", name)
		}
	}
}

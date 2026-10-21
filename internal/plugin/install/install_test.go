package install_test

import (
	"testing"

	"github.com/oronbz/visor/internal/plugin/install"
)

func TestParseSource(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want install.Source
	}{
		{"bitrise", install.Source{Owner: "oronbz", Repo: "visor-plugins", Subdir: "bitrise"}},
		{"app-store-2", install.Source{Owner: "oronbz", Repo: "visor-plugins", Subdir: "app-store-2"}},
		{"freak4pc/visor-plugins", install.Source{Owner: "freak4pc", Repo: "visor-plugins"}},
		{"owner/repo/plugins/greet", install.Source{Owner: "owner", Repo: "repo", Subdir: "plugins/greet"}},
		{"/owner/repo/", install.Source{Owner: "owner", Repo: "repo"}},
	} {
		got, err := install.ParseSource(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseSource(%q) = %+v, %v, want %+v", tc.in, got, err, tc.want)
		}
	}
}

func TestParseSourceRefusesWhatIsNeitherANameNorARepository(t *testing.T) {
	for _, in := range []string{"", "/", "Bitrise", "-bitrise", "bit_rise", ".", "..", "owner//repo", "owner/repo/../x", "owner/./repo"} {
		if got, err := install.ParseSource(in); err == nil {
			t.Errorf("ParseSource(%q) = %+v, want an error", in, got)
		}
	}
}

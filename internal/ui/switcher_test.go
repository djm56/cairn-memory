package ui

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// switcherService is a core whose project list is names, in order. It answers a read of the size
// asked, with a next cursor when names follow, and it records the limit of each read, so a test
// can count the reads a page makes.
type switcherService struct {
	stubService
	names []string
	reads []int
}

// ProjectList answers a page of the names: the limit asked, or core's default of 20 when none.
func (s *switcherService) ProjectList(_ context.Context, req core.ProjectListRequest) (*core.ProjectList, error) {
	s.reads = append(s.reads, req.Limit)
	n := req.Limit
	if n <= 0 {
		n = 20
	}
	if n > len(s.names) {
		n = len(s.names)
	}
	out := &core.ProjectList{}
	for _, name := range s.names[:n] {
		out.Projects = append(out.Projects, core.Project{Name: name})
	}
	if n < len(s.names) {
		out.Next = s.names[n-1]
	}
	return out, nil
}

// switcherNav returns the project switcher of a page, or "" when the page has none.
func switcherNav(body string) string {
	return regexp.MustCompile(`(?s)<nav class="projects".*?</nav>`).FindString(body)
}

// TestSwitcherIsBoundedAndLinksToTheProjectsPage checks the header's switcher (#156): a page with
// more projects than the cap reads one bounded page, lists the cap, and links to the full projects
// page. The name past the cap is not listed. A page whose projects all fit has no link.
func TestSwitcherIsBoundedAndLinksToTheProjectsPage(t *testing.T) {
	cases := []struct {
		name     string
		count    int
		wantMore bool
	}{
		{"more projects than the cap", switcherSize + 3, true},
		{"exactly the cap", switcherSize, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hr := newHarness(t, false, false)
			svc := &switcherService{}
			for i := 0; i < c.count; i++ {
				svc.names = append(svc.names, fmt.Sprintf("p-%02d", i))
			}
			hr.cfg.Service = svc
			hr.h = New(hr.cfg)
			cookie := hr.signIn(hr.opKey, nil)

			svc.reads = nil
			body := hr.do(http.MethodGet, "/ui/", "", cookie, nil).body.String()
			if len(svc.reads) != 1 || svc.reads[0] != switcherSize {
				t.Errorf("the page made reads %v of the project list, want one read of %d", svc.reads, switcherSize)
			}
			nav := switcherNav(body)
			if nav == "" {
				t.Fatal("the header has no project switcher")
			}
			if got := strings.Count(nav, `<a href="/ui/p/`); got != switcherSize {
				t.Errorf("the switcher lists %d projects, want the cap of %d", got, switcherSize)
			}
			if !strings.Contains(nav, fmt.Sprintf("p-%02d", switcherSize-1)) {
				t.Errorf("the switcher lacks the last name within the cap")
			}
			if strings.Contains(nav, fmt.Sprintf("p-%02d", switcherSize)) && c.count > switcherSize {
				t.Errorf("the switcher lists a project past the cap")
			}
			hasMore := strings.Contains(nav, `<a href="/ui/projects">more…</a>`)
			if hasMore != c.wantMore {
				t.Errorf("the switcher has the more link: %v, want %v", hasMore, c.wantMore)
			}
		})
	}
}

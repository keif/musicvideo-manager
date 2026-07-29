package rename_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/rename"
)

func TestRenderTemplateBasic(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{title}", rename.TemplateFields{Artist: "Bon Jovi", Title: "Wanted Dead or Alive"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Bon Jovi/Wanted Dead or Alive" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestRenderTemplateWithAlbumAndYear(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{year} - {album}/{title}", rename.TemplateFields{
		Artist: "Moby", Title: "Porcelain", Album: "Play", Year: 1999,
	})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Moby/1999 - Play/Porcelain" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestRenderTemplateSanitizesIllegalCharacters(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{title}", rename.TemplateFields{Artist: "AC/DC", Title: `Who Made Who?`})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "ACDC/Who Made Who" {
		t.Fatalf("expected illegal characters stripped, got %q", got)
	}
}

func TestRenderTemplateCollapsesEmptySegments(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{album}/{title}", rename.TemplateFields{Artist: "Alice Cooper", Title: "Poison"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Alice Cooper/Poison" {
		t.Fatalf("expected empty album segment collapsed, got %q", got)
	}
}

func TestRenderTemplateZeroYearOmitted(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{year}/{title}", rename.TemplateFields{Artist: "Beck", Title: "Loser"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Beck/Loser" {
		t.Fatalf("expected zero year segment collapsed, got %q", got)
	}
}

// TestRenderTemplateSharedSegmentDoesNotCollapseCleanly documents a known
// limitation of RenderTemplate: when a placeholder shares a path segment
// with literal text (or another placeholder) rather than occupying the
// whole segment, an empty value for that placeholder does not cleanly
// collapse -- surrounding literal text survives. This locks in the current,
// predictable-but-imperfect behavior so it can't silently regress.
func TestRenderTemplateSharedSegmentDoesNotCollapseCleanly(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{year} - {album}/{title}", rename.TemplateFields{
		Artist: "Moby", Title: "Porcelain", Album: "Play", Year: 0,
	})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Moby/- Play/Porcelain" {
		t.Fatalf("expected malformed segment from shared placeholder/literal collapse, got %q", got)
	}
}

func TestRenderTemplateEmptyTemplateErrors(t *testing.T) {
	if _, err := rename.RenderTemplate("", rename.TemplateFields{Artist: "X", Title: "Y"}); err == nil {
		t.Fatal("expected error for empty template")
	}
}

func TestRenderTemplateAllEmptyAfterSanitizeErrors(t *testing.T) {
	if _, err := rename.RenderTemplate("{album}", rename.TemplateFields{Artist: "X", Title: "Y"}); err == nil {
		t.Fatal("expected error when the rendered path has no segments left")
	}
}

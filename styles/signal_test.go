package styles

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSignalTokensMatchDesktopDarkTheme(t *testing.T) {
	want := map[string]string{
		"canvas":         "#0c0e14",
		"surface":        "#151821",
		"elevated":       "#1e2130",
		"text-primary":   "#e8ecf4",
		"text-secondary": "#8892a8",
		"border":         "#252a3a",
		"accent":         "#3b82f6",
		"success":        "#4ade80",
		"warning":        "#fbbf24",
		"danger":         "#f87171",
	}
	got := map[string]string{
		"canvas":         string(SignalCanvas),
		"surface":        string(SignalSurface),
		"elevated":       string(SignalElevated),
		"text-primary":   string(SignalTextPrimary),
		"text-secondary": string(SignalTextSecondary),
		"border":         string(SignalBorder),
		"accent":         string(SignalAccent),
		"success":        string(SignalSuccess),
		"warning":        string(SignalWarning),
		"danger":         string(SignalDanger),
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s token = %s, want %s", name, got[name], expected)
		}
	}
}

func TestSignalBodyTextMeetsContrastTarget(t *testing.T) {
	for _, background := range []lipgloss.Color{SignalCanvas, SignalSurface, SignalElevated} {
		for name, foreground := range map[string]lipgloss.Color{
			"primary":   SignalTextPrimary,
			"secondary": SignalTextSecondary,
		} {
			if ratio := contrastRatio(string(foreground), string(background)); ratio < 4.5 {
				t.Errorf("%s on %s contrast = %.2f, want at least 4.5", name, background, ratio)
			}
		}
	}
}

func TestRenderOnBackgroundRestoresBackgroundAfterNestedReset(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	child := TUIAccentText.Render("accent") + " plain"
	got := RenderOnBackground(child, SignalCanvas)
	prefix := backgroundPrefix(SignalCanvas)
	if prefix == "" {
		t.Fatal("true-color profile did not produce a background prefix")
	}
	if !strings.Contains(got, "\x1b[0m"+prefix+" plain") {
		t.Fatalf("background was not restored after child reset: %q", got)
	}
}

func contrastRatio(foreground, background string) float64 {
	a, b := relativeLuminance(foreground), relativeLuminance(background)
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

func relativeLuminance(value string) float64 {
	value = strings.TrimPrefix(value, "#")
	channels := make([]float64, 3)
	for i := range channels {
		v, _ := strconv.ParseUint(value[i*2:i*2+2], 16, 8)
		channel := float64(v) / 255
		if channel <= 0.04045 {
			channels[i] = channel / 12.92
		} else {
			channels[i] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

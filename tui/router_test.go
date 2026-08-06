package tui

import "testing"

func TestRouterPreservesTheCallingScreen(t *testing.T) {
	router := NewRouter(RouteDashboard)
	router.Push(RouteDoctor)
	router.Push(RouteMaintenance)

	if current := router.Current(); current != RouteMaintenance {
		t.Fatalf("current route = %d, want maintenance", current)
	}
	if previous, ok := router.Back(); !ok || previous != RouteDoctor {
		t.Fatalf("first back = %d, %v; want doctor, true", previous, ok)
	}
	if previous, ok := router.Back(); !ok || previous != RouteDashboard {
		t.Fatalf("second back = %d, %v; want dashboard, true", previous, ok)
	}
	if _, ok := router.Back(); ok {
		t.Fatal("dashboard root should not have a previous route")
	}
}

func TestRouterReplaceAndResetDoNotCreateFalseHistory(t *testing.T) {
	router := NewRouter(RouteSetup)
	router.Replace(RouteDashboard)
	if router.CanGoBack() {
		t.Fatal("replace should not create history")
	}
	router.Push(RouteLogs)
	router.Reset(RouteDashboard)
	if router.Current() != RouteDashboard || router.CanGoBack() {
		t.Fatalf("reset = route %d, history %v", router.Current(), router.CanGoBack())
	}
}

func TestRouterSeparatesInstallationFromControlPlane(t *testing.T) {
	for _, route := range []Route{RouteDashboard, RouteLogs, RouteSettings, RouteDoctor, RouteMaintenance, RouteRemoval} {
		if sectionForRoute(route) != SectionControlPlane {
			t.Fatalf("route %d is not in the control plane", route)
		}
	}
	if sectionForRoute(RouteSetup) != SectionInstallation {
		t.Fatal("setup route is not in the installation section")
	}

	router := NewRouter(RouteDashboard)
	router.Push(RouteSetup)
	if router.Section() != SectionInstallation {
		t.Fatal("router did not enter installation")
	}
	if _, ok := router.Back(); !ok || router.Section() != SectionControlPlane {
		t.Fatal("router did not return to the control plane")
	}
}

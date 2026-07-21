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

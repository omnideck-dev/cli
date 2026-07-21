package tui

// Route identifies one full application screen.
type Route int

const (
	RouteDashboard Route = iota
	RouteLogs
	RouteSettings
	RouteDoctor
	RouteSetup
	RouteMaintenance
)

// Router owns screen history for the TUI application shell. Workflows push a
// route when they temporarily leave the current screen; Back returns to the
// actual caller instead of assuming every journey began on the dashboard.
type Router struct {
	current Route
	history []Route
}

func NewRouter(initial Route) Router {
	return Router{current: initial}
}

func (r Router) Current() Route {
	return r.current
}

func (r Router) CanGoBack() bool {
	return len(r.history) > 0
}

func (r *Router) Push(next Route) {
	if next == r.current {
		return
	}
	r.history = append(r.history, r.current)
	r.current = next
}

// Replace changes the active route without adding a history entry. It is used
// for initial routing and completed first-run workflows.
func (r *Router) Replace(next Route) {
	r.current = next
}

// Back returns to the previous route. ok is false at the root of the app.
func (r *Router) Back() (previous Route, ok bool) {
	if len(r.history) == 0 {
		return r.current, false
	}
	last := len(r.history) - 1
	r.current = r.history[last]
	r.history = r.history[:last]
	return r.current, true
}

func (r *Router) Reset(next Route) {
	r.current = next
	r.history = nil
}

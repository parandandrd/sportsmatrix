package board

import "context"

// Show is a TV show, as a board that follows shows lists it.
type Show struct {
	ID   int
	Name string
	// Network is the channel or streaming service the show is on.
	Network string
	// Premiered is the year the show began.
	Premiered string
}

// ShowLister is a board that follows a list of TV shows, which can be changed
// while it runs.
type ShowLister interface {
	// Shows are the shows the board follows.
	Shows(ctx context.Context) []Show
	// SetShows changes the shows the board follows, once it has checked
	// that they are real, and returns them as they should be saved.
	SetShows(ctx context.Context, shows []Show) ([]string, error)
	// SearchShows finds shows by name, to choose one to follow.
	SearchShows(ctx context.Context, query string) ([]Show, error)
}

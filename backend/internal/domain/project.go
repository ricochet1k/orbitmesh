package domain

import "time"

type Project struct {
	ID        string
	Name      string
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

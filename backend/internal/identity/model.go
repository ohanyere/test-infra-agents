package identity

import (
	"errors"
	"time"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("validation error")
)

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OrganizationInput struct{ Name, Slug string }

type OrganizationSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Workspace struct {
	ID           string              `json:"id"`
	Organization OrganizationSummary `json:"organization"`
	Name         string              `json:"name"`
	Slug         string              `json:"slug"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type WorkspaceInput struct{ Name, Slug string }

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserInput struct{ Email, FullName string }

type UserSummary struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

type Membership struct {
	WorkspaceID string      `json:"workspace_id,omitempty"`
	User        UserSummary `json:"user"`
	Role        string      `json:"role"`
	JoinedAt    time.Time   `json:"joined_at"`
}

type Pagination struct{ Limit, Offset int }

type BootstrapInput struct {
	Organization OrganizationInput
	Workspace    WorkspaceInput
	User         UserInput
}

type BootstrapResult struct {
	Organization Organization `json:"organization"`
	Workspace    Workspace    `json:"workspace"`
	User         User         `json:"user"`
	Membership   Membership   `json:"membership"`
}

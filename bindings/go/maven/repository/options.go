package repository

// Options holds configuration for the Maven resource repository.
//
// TODO: add the fields this repository needs (e.g. clients, endpoints, limits) and
// a WithXxx Option constructor for each.
type Options struct{}

// Option configures Options.
type Option func(*Options)

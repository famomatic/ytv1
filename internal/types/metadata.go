package types

// Metadata contains common media metadata for embedding.
type Metadata struct {
	Title       string
	Artist      string // Author
	Description string
	Date        string // YYYY-MM-DD or YYYY
	Duration    int    // Seconds
}

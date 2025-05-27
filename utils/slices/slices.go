package slices

// TODO: Biblioteca para manejar los slice con mutex y canales

func IsEmpty[T any](slice []T) bool {
	return len(slice) == 0
}

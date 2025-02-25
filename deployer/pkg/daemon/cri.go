package daemon

type CRI interface {
	CheckAndPull(imageList []string) (failed map[string]bool, err error)
}

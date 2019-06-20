package zfs

const (
	BootfsProp     = bootfsProp
	LastUsedProp   = lastUsedProp
	SystemDataProp = systemDataProp
)

// DatasetSlice enables sorting a slice of Dataset elements
type DatasetSlice []Dataset

func (s DatasetSlice) Len() int           { return len(s) }
func (s DatasetSlice) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s DatasetSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

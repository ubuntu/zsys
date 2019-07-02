package zfs

import (
	"encoding/json"
)

// DatasetSlice enables sorting a slice of Dataset elements.
// This is the element saved and compared against.
type DatasetSlice struct {
	DS             []Dataset
	IncludePrivate bool
}

func (s DatasetSlice) Len() int           { return len(s.DS) }
func (s DatasetSlice) Less(i, j int) bool { return s.DS[i].Name < s.DS[j].Name }
func (s DatasetSlice) Swap(i, j int)      { s.DS[i], s.DS[j] = s.DS[j], s.DS[i] }

// DatasetWithSource helps marshmalling and unmarshalling to golden json files,
// exposing the "sources" elements temporarly from and to DatasetSlice
type DatasetWithSource struct {
	Dataset
	Sources *datasetSources `json:",omitempty"`
}

// Export for json Marshmalling the sources for each properties.
func (s DatasetSlice) MarshalJSON() ([]byte, error) {
	var dws []DatasetWithSource
	for _, d := range s.DS {
		datasetWS := DatasetWithSource{Dataset: d}
		if s.IncludePrivate {
			datasetWS.Sources = &datasetWS.sources
		}
		dws = append(dws, datasetWS)
	}

	return json.Marshal(dws)
}

// Import from json to export the private sources for each properties.
func (s *DatasetSlice) UnmarshalJSON(b []byte) error {
	var dws []DatasetWithSource

	if err := json.Unmarshal(b, &dws); err != nil {
		return err
	}

	for _, dw := range dws {
		d := dw.Dataset
		if s.IncludePrivate {
			d.sources = *dw.Sources
		}
		s.DS = append(s.DS, d)
	}

	return nil
}

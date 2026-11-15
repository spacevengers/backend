package firms

import (
	"fmt"
	"sort"
)

type Product struct {
	Source           string
	Sensor           string
	Product          string
	Processing       string
	DefaultSatellite string
	Description      string
}

var products = map[string]Product{
	"MODIS_NRT": {
		Source: "MODIS_NRT", Sensor: "MODIS", Product: "MCD14DL", Processing: "NRT",
		DefaultSatellite: "Terra/Aqua", Description: "MODIS near-real-time active fire (Terra/Aqua)",
	},
	"MODIS_SP": {
		Source: "MODIS_SP", Sensor: "MODIS", Product: "MCD14ML", Processing: "SP",
		DefaultSatellite: "Terra/Aqua", Description: "MODIS standard-processing active fire (Terra/Aqua)",
	},
	"VIIRS_SNPP_NRT": {
		Source: "VIIRS_SNPP_NRT", Sensor: "VIIRS", Product: "VNP14IMG", Processing: "NRT",
		DefaultSatellite: "Suomi NPP", Description: "VIIRS 375 m active fire, Suomi NPP near-real-time",
	},
	"VIIRS_SNPP_SP": {
		Source: "VIIRS_SNPP_SP", Sensor: "VIIRS", Product: "VNP14IMG", Processing: "SP",
		DefaultSatellite: "Suomi NPP", Description: "VIIRS 375 m active fire, Suomi NPP standard processing",
	},
	"VIIRS_NOAA20_NRT": {
		Source: "VIIRS_NOAA20_NRT", Sensor: "VIIRS", Product: "VJ114IMG", Processing: "NRT",
		DefaultSatellite: "NOAA-20", Description: "VIIRS 375 m active fire, NOAA-20 near-real-time",
	},
	"VIIRS_NOAA20_SP": {
		Source: "VIIRS_NOAA20_SP", Sensor: "VIIRS", Product: "VJ114IMG", Processing: "SP",
		DefaultSatellite: "NOAA-20", Description: "VIIRS 375 m active fire, NOAA-20 standard processing",
	},
	"VIIRS_NOAA21_NRT": {
		Source: "VIIRS_NOAA21_NRT", Sensor: "VIIRS", Product: "VJ214IMG", Processing: "NRT",
		DefaultSatellite: "NOAA-21", Description: "VIIRS 375 m active fire, NOAA-21 near-real-time",
	},
	"VIIRS_NOAA21_SP": {
		Source: "VIIRS_NOAA21_SP", Sensor: "VIIRS", Product: "VJ214IMG", Processing: "SP",
		DefaultSatellite: "NOAA-21", Description: "VIIRS 375 m active fire, NOAA-21 standard processing",
	},
}

func GetProduct(source string) (Product, error) {
	product, ok := products[source]
	if !ok {
		return Product{}, fmt.Errorf("unsupported FIRMS source %q", source)
	}
	return product, nil
}

func AllProducts() []Product {
	out := make([]Product, 0, len(products))
	for _, product := range products {
		out = append(out, product)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

func Sources() []string {
	out := make([]string, 0, len(products))
	for source := range products {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

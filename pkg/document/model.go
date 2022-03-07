package document

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/norwoodj/helm-docs/pkg/helm"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type valueRow struct {
	Key             string
	Type            string
	AutoDefault     string
	Default         string
	AutoDescription string
	Description     string
	Column          int
	LineNumber      int
}

type chartTemplateData struct {
	helm.ChartDocumentationInfo
	HelmDocsVersion string
	Values          []valueRow
}

func getSortedValuesTableRows(documentRoot *yaml.Node, chartValuesDescriptions map[string]helm.ChartValueDescription) ([]valueRow, error) {
	valuesTableRows, err := createValueRowsFromField(
		"",
		nil,
		documentRoot,
		chartValuesDescriptions,
		true,
	)

	if err != nil {
		return nil, err
	}

	sortOrder := viper.GetString("sort-values-order")
	if sortOrder == FileSortOrder {
		sort.Slice(valuesTableRows, func(i, j int) bool {
			if valuesTableRows[i].LineNumber == valuesTableRows[j].LineNumber {
				return valuesTableRows[i].Column < valuesTableRows[j].Column
			}

			return valuesTableRows[i].LineNumber < valuesTableRows[i].LineNumber
		})
	} else if sortOrder == AlphaNumSortOrder {
		sort.Slice(valuesTableRows, func(i, j int) bool {
			return valuesTableRows[i].Key < valuesTableRows[j].Key
		})
	} else {
		log.Warnf("Invalid sort order provided %s, defaulting to %s", sortOrder, AlphaNumSortOrder)
		sort.Slice(valuesTableRows, func(i, j int) bool {
			return valuesTableRows[i].Key < valuesTableRows[j].Key
		})
	}

	return valuesTableRows, nil
}

func getChartTemplateData(chartDocumentationInfo helm.ChartDocumentationInfo, helmDocsVersion string) (chartTemplateData, error) {
	var valuesTableRows []valueRow

	// TODO: wrap this with a loop through the list of ChartValues
	for _, chartValues := range chartDocumentationInfo.ChartValues {
		// handle empty values file case
		if chartValues.ValuesFileContents.Kind == 0 {
			continue
		}

		if chartValues.ValuesFileContents.Kind != yaml.DocumentNode {
			return chartTemplateData{}, fmt.Errorf("invalid node kind supplied: %d", chartValues.ValuesFileContents.Kind)
		}
		if chartValues.ValuesFileContents.Content[0].Kind != yaml.MappingNode {
			return chartTemplateData{}, fmt.Errorf("values file must resolve to a map, not %s", strconv.Itoa(int(chartValues.ValuesFileContents.Kind)))
		}

		rows, err := getSortedValuesTableRows(chartValues.ValuesFileContents.Content[0], *chartValues.ValuesDescriptions)
		if err != nil {
			return chartTemplateData{}, err
		}

		separatorRow := valueRow{
			Key:             "---",
			Type:            "---",
			AutoDefault:     "---",
			Default:         "---",
			AutoDescription: "---",
			Description:     chartValues.ValuesFileName,
		}

		valuesTableRows = append(valuesTableRows, separatorRow)
		valuesTableRows = append(valuesTableRows, rows...)
	}

	if valuesTableRows == nil {
		return chartTemplateData{
			ChartDocumentationInfo: chartDocumentationInfo,
			HelmDocsVersion:        helmDocsVersion,
			Values:                 make([]valueRow, 0),
		}, nil
	}

	return chartTemplateData{
		ChartDocumentationInfo: chartDocumentationInfo,
		HelmDocsVersion:        helmDocsVersion,
		Values:                 valuesTableRows,
	}, nil
}

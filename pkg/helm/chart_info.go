package helm

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var valuesDescriptionRegex = regexp.MustCompile("^\\s*#\\s*(.*)\\s+--\\s*(.*)$")
var commentContinuationRegex = regexp.MustCompile("^\\s*# (.*)$")
var defaultValueRegex = regexp.MustCompile("^\\s*# @default -- (.*)$")

type ChartMetaMaintainer struct {
	Email string
	Name  string
	Url   string
}

type ChartMeta struct {
	ApiVersion  string `yaml:"apiVersion"`
	AppVersion  string `yaml:"appVersion"`
	KubeVersion string `yaml:"kubeVersion"`
	Name        string
	Deprecated  bool
	Description string
	Version     string
	Home        string
	Type        string
	Sources     []string
	Engine      string
	Maintainers []ChartMetaMaintainer
	Annotations map[string]string
}

type ChartRequirementsItem struct {
	Name       string
	Version    string
	Repository string
	Alias      string
}

type ChartRequirements struct {
	Dependencies []ChartRequirementsItem
}

type ChartValueDescription struct {
	Description string
	Default     string
}

type ChartDocumentationInfo struct {
	ChartMeta
	ChartRequirements

	ChartDirectory string
	ChartValues    []ChartValues
}

type ChartValues struct {
	ValuesFileName     string
	ValuesFileContents *yaml.Node
	ValuesDescriptions *map[string]ChartValueDescription
}

func getYamlFileContents(filename string) ([]byte, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, err
	}

	yamlFileContents, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	return []byte(strings.Replace(string(yamlFileContents), "\r\n", "\n", -1)), nil
}

func isErrorInReadingNecessaryFile(filePath string, loadError error) bool {
	if loadError != nil {
		if os.IsNotExist(loadError) {
			log.Warnf("Required chart file %s missing. Skipping documentation for chart", filePath)
			return true
		} else {
			log.Warnf("Error occurred in reading chart file %s. Skipping documentation for chart", filePath)
			return true
		}
	}

	return false
}

func parseChartFile(chartDirectory string) (ChartMeta, error) {
	chartYamlPath := path.Join(chartDirectory, "Chart.yaml")
	chartMeta := ChartMeta{}
	yamlFileContents, err := getYamlFileContents(chartYamlPath)

	if isErrorInReadingNecessaryFile(chartYamlPath, err) {
		return chartMeta, err
	}

	err = yaml.Unmarshal(yamlFileContents, &chartMeta)
	return chartMeta, err
}

func requirementKey(requirement ChartRequirementsItem) string {
	return fmt.Sprintf("%s/%s", requirement.Repository, requirement.Name)
}

func parseChartRequirementsFile(chartDirectory string, apiVersion string) (ChartRequirements, error) {
	var requirementsPath string

	if apiVersion == "v1" {
		requirementsPath = path.Join(chartDirectory, "requirements.yaml")

		if _, err := os.Stat(requirementsPath); os.IsNotExist(err) {
			return ChartRequirements{Dependencies: []ChartRequirementsItem{}}, nil
		}
	} else {
		requirementsPath = path.Join(chartDirectory, "Chart.yaml")
	}

	chartRequirements := ChartRequirements{}
	yamlFileContents, err := getYamlFileContents(requirementsPath)

	if isErrorInReadingNecessaryFile(requirementsPath, err) {
		return chartRequirements, err
	}

	err = yaml.Unmarshal(yamlFileContents, &chartRequirements)
	if err != nil {
		return chartRequirements, err
	}

	sort.Slice(chartRequirements.Dependencies[:], func(i, j int) bool {
		return requirementKey(chartRequirements.Dependencies[i]) < requirementKey(chartRequirements.Dependencies[j])
	})

	return chartRequirements, nil
}

func parseChartValuesFile(valuesPath string) (yaml.Node, error) {
	yamlFileContents, err := getYamlFileContents(valuesPath)

	var values yaml.Node
	if isErrorInReadingNecessaryFile(valuesPath, err) {
		return values, err
	}

	err = yaml.Unmarshal(yamlFileContents, &values)
	return values, err
}

func parseChartValuesFileComments(valuesPath string) (map[string]ChartValueDescription, error) {
	valuesFile, err := os.Open(valuesPath)

	if isErrorInReadingNecessaryFile(valuesPath, err) {
		return map[string]ChartValueDescription{}, err
	}

	defer valuesFile.Close()

	keyToDescriptions := make(map[string]ChartValueDescription)
	scanner := bufio.NewScanner(valuesFile)
	foundValuesComment := false
	commentLines := make([]string, 0)

	for scanner.Scan() {
		currentLine := scanner.Text()

		// If we've not yet found a values comment with a key name, try and find one on each line
		if !foundValuesComment {
			match := valuesDescriptionRegex.FindStringSubmatch(currentLine)
			if len(match) < 3 {
				continue
			}
			if match[1] == "" {
				continue
			}

			foundValuesComment = true
			commentLines = append(commentLines, currentLine)
			continue
		}

		// If we've already found a values comment, on the next line try and parse a custom default value. If we find one
		// that completes parsing for this key, add it to the list and reset to searching for a new key
		defaultCommentMatch := defaultValueRegex.FindStringSubmatch(currentLine)
		commentContinuationMatch := commentContinuationRegex.FindStringSubmatch(currentLine)

		if len(defaultCommentMatch) > 1 || len(commentContinuationMatch) > 1 {
			commentLines = append(commentLines, currentLine)
			continue
		}

		// If we haven't continued by this point, we didn't match any of the comment formats we want, so we need to add
		// the in progress value to the map, and reset to looking for a new key
		key, description := ParseComment(commentLines)
		keyToDescriptions[key] = description
		commentLines = make([]string, 0)
		foundValuesComment = false
	}

	return keyToDescriptions, nil
}

func getChartValuesFiles(chartDirectory string, customValuesFiles []string) ([]string, error) {
	var err error
	var valuesFiles []string
	valuesFiles = append(valuesFiles, path.Join(chartDirectory, "values.yaml"))

	for _, valuesFile := range customValuesFiles {
		valuesFilePath := path.Join(chartDirectory, valuesFile)
		if _, err := os.Stat(valuesFilePath); err == nil {
			valuesFiles = append(valuesFiles, valuesFilePath)
		} else if errors.Is(err, os.ErrNotExist) {
			continue
		} else {
			log.Warnf("Something went wrong reading file path %s", valuesFilePath)
			return nil, err
		}
	}

	if err != nil {
		return valuesFiles, err
	}

	return valuesFiles, nil
}

func ParseChartInformation(chartDirectory string, customValuesFiles []string) (ChartDocumentationInfo, error) {
	var chartDocInfo ChartDocumentationInfo
	var err error

	chartDocInfo.ChartDirectory = chartDirectory
	chartDocInfo.ChartMeta, err = parseChartFile(chartDirectory)
	if err != nil {
		return chartDocInfo, err
	}

	chartDocInfo.ChartRequirements, err = parseChartRequirementsFile(chartDirectory, chartDocInfo.ApiVersion)
	if err != nil {
		return chartDocInfo, err
	}

	valuesFiles, err := getChartValuesFiles(chartDirectory, customValuesFiles)
	if err != nil {
		return chartDocInfo, err
	}

	for _, valuesFile := range valuesFiles {
		chartValues := ChartValues{
			ValuesFileName: path.Base(valuesFile),
		}

		contents, err := parseChartValuesFile(valuesFile)
		chartValues.ValuesFileContents = &contents
		if err != nil {
			return chartDocInfo, err
		}

		descriptions, err := parseChartValuesFileComments(valuesFile)
		chartValues.ValuesDescriptions = &descriptions
		if err != nil {
			return chartDocInfo, err
		}

		chartDocInfo.ChartValues = append(chartDocInfo.ChartValues, chartValues)
	}

	return chartDocInfo, nil
}

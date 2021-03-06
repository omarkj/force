package main

import (
	"archive/zip"
	"bitbucket.org/pkg/inflect"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
)

type ForceConnectedApps []ForceConnectedApp

type ForceConnectedApp struct {
	Name string `xml:"fullName"`
	Id   string `xml:"id"`
}

type ForceMetadataDeployProblem struct {
	Changed     bool   `xml:"changed"`
	Created     bool   `xml:"created"`
	Deleted     bool   `xml:"deleted"`
	Filename    string `xml:"fileName"`
	Name        string `xml:"fullName"`
	Problem     string `xml:"problem"`
	ProblemType string `xml:"problemType"`
	Success     bool   `xml:"success"`
}

type ForceMetadataQueryElement struct {
	Name    string
	Members string
}

type ForceMetadataQuery []ForceMetadataQueryElement

type ForceMetadataFiles map[string][]byte

type ForceMetadata struct {
	ApiVersion string
	Force      *Force
}

func NewForceMetadata(force *Force) (fm *ForceMetadata) {
	fm = &ForceMetadata{ApiVersion: "28.0", Force: force}
	return
}

func (fm *ForceMetadata) CheckStatus(id string) (err error) {
	body, err := fm.soapExecute("checkStatus", fmt.Sprintf("<id>%s</id>", id))
	if err != nil {
		return
	}
	var status struct {
		Done    bool   `xml:"Body>checkStatusResponse>result>done"`
		State   string `xml:"Body>checkStatusResponse>result>state"`
		Message string `xml:"Body>checkStatusResponse>result>message"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	switch {
	case !status.Done:
		return fm.CheckStatus(id)
	case status.State == "Error":
		return errors.New(status.Message)
	}
	return
}

func (fm *ForceMetadata) CheckDeployStatus(id string) (problems []ForceMetadataDeployProblem, err error) {
	body, err := fm.soapExecute("checkDeployStatus", fmt.Sprintf("<id>%s</id>", id))
	if err != nil {
		return
	}
	var result struct {
		Problems []ForceMetadataDeployProblem `xml:"Body>checkDeployStatusResponse>result>messages"`
	}
	if err = xml.Unmarshal(body, &result); err != nil {
		return
	}
	problems = result.Problems
	return
}

func (fm *ForceMetadata) CheckRetrieveStatus(id string) (files ForceMetadataFiles, err error) {
	body, err := fm.soapExecute("checkRetrieveStatus", fmt.Sprintf("<id>%s</id>", id))
	if err != nil {
		return
	}
	var status struct {
		ZipFile string `xml:"Body>checkRetrieveStatusResponse>result>zipFile"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	data, err := base64.StdEncoding.DecodeString(status.ZipFile)
	if err != nil {
		return
	}
	zipfiles, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return
	}
	files = make(map[string][]byte)
	for _, file := range zipfiles.File {
		fd, _ := file.Open()
		defer fd.Close()
		data, _ := ioutil.ReadAll(fd)
		files[file.Name] = data
	}
	return
}

func (fm *ForceMetadata) CreateConnectedApp(name, callback string) (err error) {
	soap := `
		<metadata xsi:type="ConnectedApp">
			<fullName>%s</fullName>
			<version>29.0</version>
			<label>%s</label>
			<contactEmail>%s</contactEmail>
			<oauthConfig>
				<callbackUrl>%s</callbackUrl>
				<scopes>Full</scopes>
				<scopes>RefreshToken</scopes>
			</oauthConfig>
		</metadata>
	`
	me, err := fm.Force.Whoami()
	if err != nil {
		return err
	}
	email := me["Email"]
	body, err := fm.soapExecute("create", fmt.Sprintf(soap, name, name, email, callback))
	if err != nil {
		return err
	}
	var status struct {
		Id string `xml:"Body>createResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	return
}

func (fm *ForceMetadata) CreateCustomField(object, field, typ string) (err error) {
	soap := `
		<metadata xsi:type="CustomField" xmlns:cmd="http://soap.sforce.com/2006/04/metadata">
			<fullName>%s.%s__c</fullName>
			<label>%s</label>
			%s
		</metadata>
	`
	soapField := ""
	switch strings.ToLower(typ) {
	case "text", "string":
		soapField = "<type>Text</type><length>255</length>"
	case "datetime":
		soapField = "<type>DateTime</type>"
	case "number", "int":
		soapField = "<type>Number</type><precision>10</precision><scale>0</scale>"
	case "float":
		soapField = "<type>Number</type><precision>10</precision><scale>2</scale>"
	default:
		ErrorAndExit("unable to create field type: %s", typ)
	}
	body, err := fm.soapExecute("create", fmt.Sprintf(soap, object, field, field, soapField))
	if err != nil {
		return err
	}
	var status struct {
		Id string `xml:"Body>createResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	return
}

func (fm *ForceMetadata) DeleteCustomField(object, field string) (err error) {
	soap := `
		<metadata xsi:type="CustomField" xmlns:cmd="http://soap.sforce.com/2006/04/metadata">
			<fullName>%s.%s</fullName>
		</metadata>
	`
	body, err := fm.soapExecute("delete", fmt.Sprintf(soap, object, field))
	if err != nil {
		return err
	}
	var status struct {
		Id string `xml:"Body>deleteResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	return
}

func (fm *ForceMetadata) CreateCustomObject(object string) (err error) {
	soap := `
		<metadata xsi:type="CustomObject" xmlns:cmd="http://soap.sforce.com/2006/04/metadata">
			<fullName>%s__c</fullName>
			<label>%s</label>
			<pluralLabel>%s</pluralLabel>
			<deploymentStatus>Deployed</deploymentStatus>
			<sharingModel>ReadWrite</sharingModel>
			<nameField>
				<label>ID</label>
				<type>AutoNumber</type>
			</nameField>
		</metadata>
	`
	body, err := fm.soapExecute("create", fmt.Sprintf(soap, object, object, inflect.Pluralize(object)))
	if err != nil {
		return err
	}
	var status struct {
		Id string `xml:"Body>createResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	return
}

func (fm *ForceMetadata) DeleteCustomObject(object string) (err error) {
	soap := `
		<metadata xsi:type="CustomObject" xmlns:cmd="http://soap.sforce.com/2006/04/metadata">
			<fullName>%s</fullName>
		</metadata>
	`
	body, err := fm.soapExecute("delete", fmt.Sprintf(soap, object))
	if err != nil {
		return err
	}
	var status struct {
		Id string `xml:"Body>deleteResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	return
}

func (fm *ForceMetadata) Deploy(files ForceMetadataFiles) (problems []ForceMetadataDeployProblem, err error) {
	soap := `
		<zipFile>%s</zipFile>
	`
	zipfile := new(bytes.Buffer)
	zipper := zip.NewWriter(zipfile)
	for name, data := range files {
		wr, err := zipper.Create(fmt.Sprintf("unpackaged/%s", name))
		if err != nil {
			return nil, err
		}
		wr.Write(data)
	}
	zipper.Close()
	encoded := base64.StdEncoding.EncodeToString(zipfile.Bytes())
	body, err := fm.soapExecute("deploy", fmt.Sprintf(soap, encoded))
	if err != nil {
		return
	}
	var status struct {
		Id string `xml:"Body>deployResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	messages, err := fm.CheckDeployStatus(status.Id)
	for _, problem := range messages {
		if !problem.Success {
			problems = append(problems, problem)
		}
	}
	return
}

func (fm *ForceMetadata) Retrieve(query ForceMetadataQuery) (files ForceMetadataFiles, err error) {
	soap := `
		<retrieveRequest>
			<apiVersion>29.0</apiVersion>
			<unpackaged>
				%s
			</unpackaged>
		</retrieveRequest>
	`
	soapType := `
		<types>
			<name>%s</name>
			<members>%s</members>
		</types>
	`
	types := ""
	for _, element := range query {
		types += fmt.Sprintf(soapType, element.Name, element.Members)
	}
	body, err := fm.soapExecute("retrieve", fmt.Sprintf(soap, types))
	if err != nil {
		return
	}
	var status struct {
		Id string `xml:"Body>retrieveResponse>result>id"`
	}
	if err = xml.Unmarshal(body, &status); err != nil {
		return
	}
	if err = fm.CheckStatus(status.Id); err != nil {
		return
	}
	raw_files, err := fm.CheckRetrieveStatus(status.Id)
	if err != nil {
		return
	}
	files = make(ForceMetadataFiles)
	for raw_name, data := range raw_files {
		name := strings.Replace(raw_name, "unpackaged/", "", -1)
		files[name] = data
	}
	return
}

func (fm *ForceMetadata) ListMetadata(query string) (res []byte, err error) {
	return fm.soapExecute("listMetadata", fmt.Sprintf("<queries><type>%s</type></queries>", query))
}

func (fm *ForceMetadata) ListConnectedApps() (apps ForceConnectedApps, err error) {
	originalVersion := fm.ApiVersion
	fm.ApiVersion = "29.0"
	body, err := fm.ListMetadata("ConnectedApp")
	fm.ApiVersion = originalVersion
	if err != nil {
		return
	}
	var res struct {
		ConnectedApps []ForceConnectedApp `xml:"Body>listMetadataResponse>result"`
	}
	if err = xml.Unmarshal(body, &res); err != nil {
		return
	}
	apps = res.ConnectedApps
	return
}

func (fm *ForceMetadata) soapExecute(action, query string) (response []byte, err error) {
	login, err := fm.Force.Get(fm.Force.Credentials.Id)
	if err != nil {
		return
	}
	url := strings.Replace(login["urls"].(map[string]interface{})["metadata"].(string), "{version}", fm.ApiVersion, 1)
	soap := NewSoap(url, "http://soap.sforce.com/2006/04/metadata", fm.Force.Credentials.AccessToken)
	response, err = soap.Execute(action, query)
	return
}

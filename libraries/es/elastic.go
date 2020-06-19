package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/olivere/elastic"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"time"
	"gin-frame/libraries/config"
	"gin-frame/libraries/log"
	"gin-frame/libraries/util"
)

type Elastic struct {
	Client *elastic.Client
	host   string
}
/*
es 缓存池
 */
var (
	ElasticPool = make(map[string]*Elastic)
)

func InitES(name string) *Elastic {
	if es, ok := ElasticPool[name]; ok {
		return es
	}
	confES := config.GetConfig("es", name)
	host := confES.Key("host").String() + ":" + confES.Key("port").String()
	var err error
	client, err := elastic.NewClient(elastic.SetURL(host))
	if err != nil {
		panic(err)
	}
	info, code, err := client.Ping(host).Do(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("Elasticsearch returned with code %d and version %s\n", code, info.Version.Number)

	esVersion, err := client.ElasticsearchVersion(host)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Elasticsearch version %s\n", esVersion)
	es := &Elastic{
		Client: client,
		host: host,
	}
	ElasticPool[name] = es
	return es
}
/*
新建索引
 */
func (self *Elastic)CreateIndex(index, mapping string, logHeader *log.LogFormat) bool {
	// 判断索引是否存在
	exists, err := self.Client.IndexExists(index).Do(context.Background())
	if err != nil {
		log.Errorf(logHeader, "<CreateIndex> some error occurred when check exists, index: %s, err:%s", index, err.Error())
		return false
	}
	if exists {
		log.Infof(logHeader, "<CreateIndex> index:{%s} is already exists", index)
		return true
	}
	createIndex, err := self.Client.CreateIndex(index).Body(mapping).Do(context.Background())
	if err != nil {
		log.Errorf(logHeader, "<CreateIndex> some error occurred when create. index: %s, err:%s", index, err.Error())
		return false
	}
	if !createIndex.Acknowledged {
		// Not acknowledged
		log.Errorf(logHeader, "<CreateIndex> Not acknowledged, index: %s", index)
		return false
	}
	return true
}
/*
删除索引
 */
func (self *Elastic)DelIndex(index string, logHeader *log.LogFormat) bool {
	// Delete an index.
	deleteIndex, err := self.Client.DeleteIndex(index).Do(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(logHeader, "<DelIndex> some error occurred when delete. index: %s, err:%s", index, err.Error())
		return false
	}
	if !deleteIndex.Acknowledged {
		// Not acknowledged
		log.Errorf(logHeader, "<DelIndex> acknowledged. index: %s", index)
		return false
	}
	return true
}
/*
数据存储
 */
func (self *Elastic)Put(index, typ, id, bodyJson string, logHeader *log.LogFormat) bool {
	put, err := self.Client.Index().
		Index(index).
		Type(typ).
		Id(id).
		BodyJson(bodyJson).
		Do(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(logHeader, "<Put> some error occurred when put.  err:%s", err.Error())
		return false
	}
	log.Infof(logHeader, "<Put> success, id: %s to index: %s, type %s\n", put.Id, put.Index, put.Type)
	return true
}
/*
数据删除
*/
func (self *Elastic)Del(index, typ, id string, logHeader *log.LogFormat) bool {
	del, err := self.Client.Delete().
		Index(index).
		Type(typ).
		Id(id).
		Do(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(logHeader, "<Del> some error occurred when put.  err:%s", err.Error())
		return false
	}
	log.Infof(logHeader, "<Del> success, id: %s to index: %s, type %s\n", del.Id, del.Index, del.Type)
	return true
}
/*
更新数据
 */
func (self *Elastic)Update(index, typ, id string, updateMap map[string]interface{}, logHeader *log.LogFormat) bool {
	res, err := self.Client.Update().
		Index(index).Type(typ).Id(id).
		Doc(updateMap).
		FetchSource(true).
		Do(context.Background())
	if err != nil {
		log.Errorf(logHeader, "<Update> some error occurred when update. index:%s, typ:%s, id:%s err:%s", index, typ, id, err.Error())
		return false
	}
	if res == nil {
		log.Errorf(logHeader, "<Update> expected response != nil. index:%s, typ:%s, id:%s", index, typ, id)
		return false
	}
	if res.GetResult == nil {
		log.Errorf(logHeader, "<Update> expected GetResult != nil. index:%s, typ:%s, id:%s", index, typ, id)
		return false
	}
	data, _ := json.Marshal(res.GetResult.Source)
	log.Errorf(logHeader, "<Update> update success. data:%s", data)
	return true
}

func (self *Elastic)TermQueryMap(index, typ string, term *elastic.TermQuery, start, end int, logHeader *log.LogFormat) map[string]interface{} {
	//elastic.NewTermQuery()
	searchResult, err := self.Client.Search().
		Index(index).
		Type(typ).
		Query(term).        // specify the query
		From(start).Size(end).        // take documents start-end
		Pretty(true).            // pretty print request and response JSON
		DoString(context.Background()) // execute
	if err != nil {
		fmt.Println(err.Error())
		// Handle error
		log.Errorf(logHeader, "<TermQuery> some error occurred when search. index:%s, term:%v,  err:%s", index, term, err.Error())
		return make(map[string]interface{})
	}
	return util.JsonToMap(searchResult)
}

func (self *Elastic)TermQuery(index, typ string, term *elastic.TermQuery, start, end int, logHeader *log.LogFormat) *elastic.SearchResult {
	//elastic.NewTermQuery()
	searchResult, err := self.Client.Search().
		Index(index).
		Type(typ).
		Query(term).        // specify the query
		From(start).Size(end).        // take documents start-end
		Pretty(true).            // pretty print request and response JSON
		Do(context.Background()) // execute
	if err != nil {
		fmt.Println(err.Error())
		// Handle error
		log.Errorf(logHeader, "<TermQuery> some error occurred when search. index:%s, term:%v,  err:%s", index, term, err.Error())
		return nil
	}
	return searchResult
}

func (self *Elastic)QueryStringMap(index, typ, query string, start,end int, header *log.LogFormat) map[string]interface{} {
	q := elastic.NewQueryStringQuery(query)
	// Match all should return all documents
	searchResult, err := self.Client.Search().
		Index(index).
		Type(typ).    // type of Index
		Query(q).
		Pretty(true).
		From(start).Size(end).
		DoString(context.Background())
	if err != nil {
		fmt.Println(err.Error())
		// Handle error
		log.Errorf(header, "<QueryString> some error occurred when search. index:%s, query:%v,  err:%s", index, query, err.Error())
		return nil
	}
	return util.JsonToMap(searchResult)
}

// https://www.elastic.co/guide/en/elasticsearch/reference/6.8/query-dsl-query-string-query.html
func (self *Elastic)QueryString(index, typ, query string, size int, header *log.LogFormat) *elastic.SearchResult {

	q := elastic.NewQueryStringQuery(query)
	// Match all should return all documents
	searchResult, err := self.Client.Search().
		Index(index).
		Type(typ).    // type of Index
		Query(q).
		Pretty(true).
		From(0).Size(size).
		Do(context.Background())
	if err != nil {
		fmt.Println(err.Error())
		// Handle error
		log.Errorf(header, "<QueryString> some error occurred when search. index:%s, query:%v,  err:%s", index, query, err.Error())
		return nil
	}
	return searchResult
}

/*
多条件参考：https://stackoverflow.com/questions/49942373/golang-elasticsearch-multiple-query-parameters
 */
func (self *Elastic)MultiMatchQueryBestFields(index, typ, text string, start,end int,logHeader *log.LogFormat,fields ...string) *elastic.SearchResult {
	q := elastic.NewMultiMatchQuery(text, fields...)
	searchResult, err := self.Client.Search().
		Index(index). // name of Index
		Type(typ).    // type of Index
		Query(q).
		Sort("_score", false).
		From(start).Size(end).
		Do(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(logHeader, "<MultiMatchQueryBestFields> some error occurred when search. index:%s, text:%v,  err:%s", index, text, err.Error())
		return nil
	}
	return searchResult
}

func (self *Elastic)MultiMatchQueryBestFieldsMap(index, typ, text string, start,end int,logHeader *log.LogFormat,fields ...string) map[string]interface{} {
	q := elastic.NewMultiMatchQuery(text, fields...)
	searchResult, err := self.Client.Search().
		Index(index). // name of Index
		Type(typ).    // type of Index
		Query(q).
		Sort("_score", false).
		From(start).Size(end).
		DoString(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(logHeader, "<MultiMatchQueryBestFields> some error occurred when search. index:%s, text:%v,  err:%s", index, text, err.Error())
		return nil
	}
	return util.JsonToMap(searchResult)
}

func (self *Elastic)QueryStringRandomSearch(client *elastic.Client, index, typ, query string, size int, header *log.LogFormat) *elastic.SearchResult {
	q := elastic.NewFunctionScoreQuery()
	queryString := elastic.NewQueryStringQuery(query)
	q = q.Query(queryString)
	q = q.AddScoreFunc(elastic.NewRandomFunction())
	searchResult, err := client.Search().
		Index(index).
		Type(typ).
		Query(q).
		Size(size).
		Do(context.Background())
	if err != nil {
		// Handle error
		log.Errorf(header, "<QueryStringRandomSearch> some error occurred when search. index:%s, query:%v,  err:%s", index, query, err.Error())
		return nil
	}
	return searchResult
}

func (self *Elastic)RangeQueryLoginDate(index string, typ string, start,end int, logHeader *log.LogFormat) *elastic.SearchResult {
	q := elastic.NewRangeQuery("latest_time").
		Gte("now-30d/d")
	searchResult, err := self.Client.Search().
		Index(index).
		Type(typ).
		Query(q).
		Sort("latest_time", false).
		From(start).Size(end).
		Do(context.Background())
	if err != nil {
		log.Errorf(logHeader, "<RangeQueryLoginDate> some error occurred when search. index:%s,err:%s", index, err.Error())
		return nil
	}
	return searchResult
}

func (self *Elastic)JsonMap(index,typ,query string, fields []string, from, size int,
	terms map[string]interface{}, mustNot, filter,sort []map[string]interface{}, logHeader *log.LogFormat) map[string]interface{}{
	data := make(map[string]interface{})

	var must []map[string]interface{}
	if len(terms) != 0 {
		must = append(must, map[string]interface{}{
			"terms":terms,
		})
	}
	if query != "" {
		multiMatch := make(map[string]interface{})
		multiMatch["query"] = query
		if len(fields) != 0 {
			multiMatch["fields"] = fields
		}
		must = append(must, map[string]interface{}{
			"multi_match":multiMatch,
		})
	}
	data["from"] = from
	data["size"] = size
	data["sort"] = sort
	data["query"] = map[string]interface{}{
		"bool": map[string]interface{}{
			"must": must,
			"must_not":mustNot,
			"filter":filter,
		},
	}

	byteDates, err := json.Marshal(data)
	util.Must(err)
	reader := bytes.NewReader(byteDates)

	client := &http.Client{}
	url := self.host + "/" + index + "/" + typ + "/_search"
	req, err := http.NewRequest("POST", url, reader)
	util.Must(err)

	req.Header.Add("content-type", "application/json")

	resp, err := client.Do(req)
	util.Must(err)
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	util.Must(err)

	ret := util.JsonToMap(string(b))
	if ret["error"] != nil {
		fmt.Println(fmt.Sprintf("%s", string(b)))
		log.Errorf(logHeader, fmt.Sprintf("%s", string(b)) )
		panic("es error")
		return nil
	}

	return ret
}

func (self *Elastic)JsonMapHttp(index,typ,query string, fields []string, from, size int,
	terms map[string]interface{}, mustNot, filter,sort []map[string]interface{}, logHeader *log.LogFormat, ctx context.Context) map[string]interface{}{

	var (
		parent        = opentracing.SpanFromContext(ctx)
		operationName = "es_search"
		statement 	  = fmt.Sprintf("esUri:%s, index:%s, type:%s, query:%s",
			self.host, index, typ, query)
		span          = func() opentracing.Span {
			if parent == nil {
				return opentracing.StartSpan(operationName)
			}
			return opentracing.StartSpan(operationName, opentracing.ChildOf(parent.Context()))
		}()
		logFormat  = log.LogHeaderFromContext(ctx)
		startAt    = time.Now()
		endAt      time.Time
	)
	var err error

	lastModule := logFormat.Module
	defer func() {logFormat.Module = lastModule}()

	defer span.Finish()
	defer func() {
		endAt = time.Now()

		logFormat.StartTime = startAt
		logFormat.EndTime = endAt
		latencyTime := logFormat.EndTime.Sub(logFormat.StartTime).Microseconds()// 执行时间
		logFormat.LatencyTime = latencyTime

		span.SetTag("error", err != nil)
		span.SetTag("es.type", "search")
		span.SetTag("es.statement", statement)

		if err != nil {
			log.Errorf(logFormat, "%s:[%s], error: %s", operationName, statement, err)
		}else {
			log.Warnf(logFormat, statement)
		}

		logFormat.Module = "databus/es"
	}()

	if parent == nil {
		span = opentracing.StartSpan("esDo")
	} else {
		span = opentracing.StartSpan("esDo", opentracing.ChildOf(parent.Context()))
	}
	defer span.Finish()

	span.SetTag("db.type", "redis")
	span.SetTag("db.statement", statement)
	span.SetTag("error", err != nil)

	data := make(map[string]interface{})

	var must []map[string]interface{}
	if len(terms) != 0 {
		must = append(must, map[string]interface{}{
			"terms":terms,
		})
	}
	if query != "" {
		multiMatch := make(map[string]interface{})
		multiMatch["query"] = query
		if len(fields) != 0 {
			multiMatch["fields"] = fields
		}
		must = append(must, map[string]interface{}{
			"multi_match":multiMatch,
		})
	}
	data["from"] = from
	data["size"] = size
	data["sort"] = sort
	data["query"] = map[string]interface{}{
		"bool": map[string]interface{}{
			"must": must,
			"must_not":mustNot,
			"filter":filter,
		},
	}

	byteDates, err := json.Marshal(data)
	util.Must(err)
	reader := bytes.NewReader(byteDates)

	client := &http.Client{}
	url := self.host + "/" + index + "/" + typ + "/_search"
	req, err := http.NewRequest("POST", url, reader)
	util.Must(err)

	req.Header.Add("content-type", "application/json")

	resp, err := client.Do(req)
	util.Must(err)
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	util.Must(err)

	ret := util.JsonToMap(string(b))
	if ret["error"] != nil {
		err = errors.New(fmt.Sprintf("%s", string(b)))
		log.Errorf(logHeader, fmt.Sprintf("%s", string(b)) )
		return nil
	}

	return ret
}
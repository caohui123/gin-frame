package panic

import (
	"bytes"
	"gin-frame/codes"
	"gin-frame/libraries/log"
	"gin-frame/libraries/util"
	"gin-frame/libraries/util/conversion"
	"gin-frame/libraries/util/url"
	"gin-frame/libraries/xhop"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func ThrowPanic(port int, logFields map[string]string, dir string, area int, productName, moduleName, env string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func(c *gin.Context) {
			if err := recover(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"errno":    codes.SERVER_ERROR,
					"errmsg":   codes.ErrorMsg[codes.SERVER_ERROR],
					"data":     make(map[string]interface{}),
					"user_msg": codes.ErrorUserMsg[codes.SERVER_ERROR],
				})

				debugStack := make(map[int]interface{})
				for k, v := range strings.Split(string(debug.Stack()), "\n") {
					debugStack[k] = v
				}

				file := util.CreateDateDir(dir, moduleName+".err."+util.HostName()+".")
				file = file + "/" + strconv.Itoa(util.RandomN(area))

				log.Init(&log.LogConfig{
					File:           file,
					Path:           dir,
					Mode:           1,
					AsyncFormatter: false,
					Debug:          true,
				}, dir, file)

				var logID string
				switch {
				case c.Query(logFields["query_id"]) != "":
					logID = c.Query(logFields["query_id"])
				case c.Request.Header.Get(logFields["header_id"]) != "":
					logID = c.Request.Header.Get(logFields["header_id"])
				default:
					logID = log.NewObjectId().Hex()
				}

				logHeader := &log.LogFormat{}
				ctx := c.Request.Context()
				dst := new(log.LogFormat)
				*dst = *logHeader

				dst.Port = port
				dst.LogId = logID
				dst.Method = c.Request.Method
				dst.CallerIp = c.ClientIP()
				dst.UriPath = c.Request.RequestURI
				dst.XHop = xhop.NextXhop(c.Request.Header, logFields["header_hop"])
				dst.Product = productName
				dst.Module = moduleName
				dst.Env = env

				ctx = log.ContextWithLogHeader(ctx, dst)
				c.Request = c.Request.WithContext(ctx)
				c.Writer.Header().Set(logFields["header_id"], dst.LogId)
				c.Writer.Header().Set(logFields["header_hop"], dst.XHop.String())

				reqBody := []byte{}
				if c.Request.Body != nil { // Read
					reqBody, _ = ioutil.ReadAll(c.Request.Body)
				}
				strReqBody := string(reqBody)

				c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(reqBody)) // Reset
				responseWriter := &bodyLogWriter{body: bytes.NewBuffer(nil), ResponseWriter: c.Writer}
				c.Writer = responseWriter

				dst.StartTime = time.Now()

				dst.HttpCode = c.Writer.Status()

				responseBody := responseWriter.body.String()

				log.Error(dst, map[string]interface{}{
					"requestHeader": c.Request.Header,
					"requestBody":   conversion.JsonToMap(strReqBody),
					"responseBody":  conversion.JsonToMap(responseBody),
					"uriQuery":      url.ParseUriQueryToMap(c.Request.URL.RawQuery),
					"err":           err,
					"trace":         debugStack,
				})

				/* util.WriteWithIo(file,"[" +dateTime+"]")
				util.WriteWithIo(file, fmt.Sprintf("%v\r\n", err))
				util.WriteWithIo(file, debugStack) */
				c.Done()
			}
		}(c)
		c.Next()
	}
}

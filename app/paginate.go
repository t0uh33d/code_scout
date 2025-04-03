package app

import (
	"context"
	"encoding/json"

	"net/http"
	"strconv"
	"strings"

	"github.com/t0uh33d/code_scout/utils"
)

const ContextKeyPagination string = "Pagination"
const ContextKeySorting string = "Sorting"
const ContextKeyFiltering string = "Filtering"

type injectResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	responseData []byte
}

func NewInjectieveResponseWriter(w http.ResponseWriter) *injectResponseWriter {
	return &injectResponseWriter{w, http.StatusOK, []byte{}}
}

func (lrw *injectResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *injectResponseWriter) Write(b []byte) (int, error) {
	//Do not write actual result until we are sure about it
	lrw.responseData = b
	return 0, nil
}

// Paginate middleware transforms paginated data from URL to context of the request.
// Parameters can be all=true , page=0&per_page=5 , page=3&per_page=10
var Paginate = func(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		all := false
		var page uint = 0
		var perPage uint = utils.DefaultPerPage

		allParam, ok := r.URL.Query()["all"]
		if ok {
			if ok && allParam[0] == "true" {
				all = true
			}
		} else { //if all parameter is provided ignore others
			paginationParamsProvided := false

			pageParam, pageOK := r.URL.Query()["page"]
			if pageOK {
				pageParsed, err := strconv.ParseUint(pageParam[0], 10, 64)
				if nil == err {
					paginationParamsProvided = true
					page = uint(pageParsed)
				}
			}

			perPageParam, perPageOK := r.URL.Query()["per_page"]
			if perPageOK {
				perPageParsed, err := strconv.ParseUint(perPageParam[0], 10, 64)
				if nil == err {
					paginationParamsProvided = true
					perPage = uint(perPageParsed)
					if perPage == 0 {
						perPage = 1
					}
				}
			}
			if !paginationParamsProvided {
				all = true
			}
		}

		pagination := utils.Pagination{
			Page:    page,
			PerPage: perPage,
			All:     all,
		}
		ctx = context.WithValue(ctx, ContextKeyPagination, &pagination)

		// The rest of the code is for backwards compatibility in order to return
		// just the items instead of the extra metadata.
		// To revert it replace with the : next.ServeHTTP(w, r.WithContext(ctx))
		lrw := NewInjectieveResponseWriter(w)
		next.ServeHTTP(lrw, r.WithContext(ctx))

		//if not successful or not requested all items
		if !utils.IsSuccessfulStatusCode(lrw.statusCode) || !all {
			w.Write(lrw.responseData)
			return
		}

		var result map[string]interface{}
		err := json.Unmarshal(lrw.responseData, &result)
		if err != nil {
			w.Write(lrw.responseData)
			return
		}
		items, ok := result["items"]
		if !ok {
			w.Write(lrw.responseData)
			return
		}
		dt, err := json.Marshal(items)
		if !ok {
			w.Write(lrw.responseData)
			return
		}
		w.Write(dt)
	}
}

// Sort middleware transforms sorting data from URL to context of the request.
// Parameters can be sort=name&order=asc, sort=age&order=desc
var Sort = func(next http.HandlerFunc, defaultSort string, defaultOrder utils.SortOrderBy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		var sort = defaultSort
		var order = defaultOrder

		sortParam, sortOK := r.URL.Query()["sort"]
		if sortOK {
			sort = sortParam[0]

		}
		orderParam, orderOK := r.URL.Query()["order"]
		if orderOK {
			if strings.EqualFold(orderParam[0], string(utils.OrderByASC)) {
				order = utils.OrderByASC

			} else if strings.EqualFold(orderParam[0], string(utils.OrderByDESC)) {
				order = utils.OrderByDESC
			}
		}

		sorting := utils.Sorting{
			Sort:  sort,
			Order: order,
		}
		ctx := r.Context()
		ctx = context.WithValue(ctx, ContextKeySorting, &sorting)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// Filter middleware transforms filtering data from URL to context of the request.
// Parameters can be email[eqstr]=an@email.com , age[gtenum]=30 , username[peqstr]=test
var Filter = func(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		filtering := utils.Filtering{}

		for paramName, paramValue := range r.URL.Query() {
			paramNameSplit := strings.Split(paramName, "[")
			if len(paramNameSplit) != 2 {
				continue
			}
			if !strings.HasSuffix(paramNameSplit[1], "]") {
				continue
			}
			filterAttribute := paramNameSplit[0]
			paramOperation := strings.TrimSuffix(paramNameSplit[1], "]")
			if len(paramOperation) < 5 {
				continue
			}
			var filterValueType utils.FilterValueType
			var filterValue interface{}
			if strings.EqualFold(paramOperation[len(paramOperation)-3:], string(utils.FilterValueTypeNumber)) {
				filterValueType = utils.FilterValueTypeNumber
				paramValueParsed, err := strconv.ParseFloat(paramValue[0], 64)
				if err != nil {
					continue
				}
				filterValue = paramValueParsed
			} else if strings.EqualFold(paramOperation[len(paramOperation)-3:], string(utils.FilterValueTypeString)) {
				filterValueType = utils.FilterValueTypeString
				filterValue = paramValue[0]
			} else {
				continue
			}

			var filterOperation utils.FilterOperation
			operation := paramOperation[:len(paramOperation)-3]
			if strings.EqualFold(operation, string(utils.FilterOperationEQ)) {
				filterOperation = utils.FilterOperationEQ
			} else if strings.EqualFold(operation, string(utils.FilterOperationPEQ)) {
				filterOperation = utils.FilterOperationPEQ
			} else if strings.EqualFold(operation, string(utils.FilterOperationNEQ)) {
				filterOperation = utils.FilterOperationNEQ
			} else if strings.EqualFold(operation, string(utils.FilterOperationGT)) {
				filterOperation = utils.FilterOperationGT
			} else if strings.EqualFold(operation, string(utils.FilterOperationGTE)) {
				filterOperation = utils.FilterOperationGTE
			} else if strings.EqualFold(operation, string(utils.FilterOperationLT)) {
				filterOperation = utils.FilterOperationLT
			} else if strings.EqualFold(operation, string(utils.FilterOperationLTE)) {
				filterOperation = utils.FilterOperationLTE
			} else {
				continue
			}

			filter := utils.Filter{
				Attribute: filterAttribute,
				Operation: filterOperation,
				ValueType: filterValueType,
				Value:     filterValue,
			}
			filtering.Filters = append(filtering.Filters, filter)
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, ContextKeyFiltering, &filtering)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

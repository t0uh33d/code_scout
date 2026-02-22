package utils

import (
	"fmt"
	"math"
	"strings"

	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

const DefaultPerPage = 10

type SortOrderBy string

const OrderByASC = SortOrderBy("asc")
const OrderByDESC = SortOrderBy("desc")

type FilterOperation string
type FilterValueType string

const FilterValueTypeNumber = FilterValueType("num")
const FilterValueTypeString = FilterValueType("str")

const FilterOperationEQ = FilterOperation("eq")
const FilterOperationPEQ = FilterOperation("peq")
const FilterOperationNEQ = FilterOperation("neq")
const FilterOperationGT = FilterOperation("gt")
const FilterOperationGTE = FilterOperation("gte")
const FilterOperationLT = FilterOperation("lt")
const FilterOperationLTE = FilterOperation("lte")

type Pagination struct {
	Page    uint
	PerPage uint
	All     bool
}

type PaginationResult struct {
	Page       uint `json:"page"`
	PerPage    uint `json:"per_page"`
	TotalItems uint `json:"total_items"`
	TotalPages uint `json:"total_pages"`
}

type Sorting struct {
	Sort         string
	Order        SortOrderBy
	AllowedSorts map[string]string
}

type Filtering struct {
	Filters        []Filter
	AllowedFilters map[string]string
}

type Filter struct {
	Attribute string
	Operation FilterOperation
	ValueType FilterValueType
	Value     interface{}
}

func (pag *Pagination) Paginate(tx *gorm.DB) *gorm.DB {
	if pag != nil && pag.All != true {
		tx = tx.Limit(int(pag.PerPage)).Offset(int(pag.Page * pag.PerPage))
	}
	return tx
}

func (pag *Pagination) PaginateRaw() string {
	if pag != nil && pag.All != true {
		return " LIMIT " + fmt.Sprint(pag.PerPage) + " OFFSET " + fmt.Sprint(pag.Page*pag.PerPage)
	}
	return ""
}

func (sort *Sorting) SortByRaw() string {
	if sort != nil && len(sort.Sort) > 0 && len(sort.Order) > 0 {
		ns := getNamespaceForTheGivenColumn(sort.Sort, sort.AllowedSorts)

		if len(sort.AllowedSorts) > 0 {
			if allowed := sort.Validate(&sort.Sort); !allowed {
				cslog.Debugf("Invalid sort => %s", sort.Sort)
				cslog.Debugf("The valid ones are %v", sort.AllowedSorts)
				return ""
			} else {
				return " ORDER BY " + ns + sort.Sort + " " + string(sort.Order)
			}
		}
		return " ORDER BY " + ns + sort.Sort + " " + string(sort.Order)
	}

	return ""
}

func (sort *Sorting) SortBy(tx *gorm.DB) *gorm.DB {
	if tx == nil {
		return nil
	}

	if sort != nil && len(sort.Sort) > 0 && len(sort.Order) > 0 {
		tx = tx.Order(sort.Sort + " " + string(sort.Order))
		if len(sort.AllowedSorts) > 0 {
			if allowed := sort.Validate(&sort.Sort); !allowed {
				cslog.Debugf("Invalid sort => %s", sort.Sort)
				cslog.Debugf("The valid ones are %v", sort.AllowedSorts)
				return tx
			}

		}
	}
	return tx
}

func (filters *Filtering) FilterBy(tx *gorm.DB) *gorm.DB {
	if tx == nil || filters == nil {
		return tx
	}

	for _, filter := range filters.Filters {

		if allowed := filters.Validate(filter, false); !allowed {
			fmt.Println("Invalid filter => ", filter)
			fmt.Println("The valid ones are ", filters.AllowedFilters)
			continue
		}

		op := ""
		if len(filter.Attribute) == 0 {
			continue
		}
		switch filter.Operation {
		case FilterOperationEQ:
			op = "="
		case FilterOperationNEQ:
			op = "<>"
		case FilterOperationPEQ:
			op = "LIKE"
		case FilterOperationLT:
			op = "<"
		case FilterOperationLTE:
			op = "<="
		case FilterOperationGT:
			op = ">"
		case FilterOperationGTE:
			op = ">="
		default:
			continue
		}
		if filter.ValueType == FilterValueTypeNumber {
			tx = tx.Where(filter.Attribute+" "+op+" ?", filter.Value.(float64))
		} else {
			if filter.Operation == FilterOperationPEQ {
				tx = tx.Where(filter.Attribute+" "+op+" ?", "%"+filter.Value.(string)+"%")
			} else {
				tx = tx.Where(filter.Attribute+" "+op+" ?", filter.Value.(string))
			}
		}
	}
	return tx
}

func (filters *Filtering) FilterByRaw() (string, []interface{}) {
	if filters == nil {
		fmt.Println("Filters are empty")
		return "", nil
	}
	var conditions []string
	var args []interface{}

	for _, filter := range filters.Filters {
		if len(filter.Attribute) == 0 {
			continue
		}
		if allowed := filters.Validate(filter, true); !allowed {
			cslog.Debugf("Invalid filter => %s", filter)
			cslog.Debugf("The valid ones are %v", filters.AllowedFilters)
			continue
		}

		op := ""
		switch filter.Operation {
		case FilterOperationEQ:
			op = "="
		case FilterOperationNEQ:
			op = "<>"
		case FilterOperationPEQ:
			op = "LIKE"
		case FilterOperationLT:
			op = "<"
		case FilterOperationLTE:
			op = "<="
		case FilterOperationGT:
			op = ">"
		case FilterOperationGTE:
			op = ">="
		default:
			continue
		}

		ns := getNamespaceForTheGivenColumn(filter.Attribute, filters.AllowedFilters)

		var column string
		if strings.Contains(ns, ".") {
			afterDot := ns[strings.Index(ns, ".")+1:]
			if len(afterDot) > 0 {
				column = ns
				column = strings.TrimSuffix(column, ".")
			} else {
				column = fmt.Sprintf("%s%s", ns, filter.Attribute)
			}
		}

		var condition string
		if filter.ValueType == FilterValueTypeNumber {
			condition = fmt.Sprintf("%s %s ?", column, op)
			args = append(args, filter.Value.(float64))
		} else {
			if filter.Operation == FilterOperationPEQ {
				condition = fmt.Sprintf("%s %s ?", column, op)
				args = append(args, "%"+filter.Value.(string)+"%")
			} else {
				condition = fmt.Sprintf("%s %s ?", column, op)
				args = append(args, filter.Value.(string))
			}
		}
		conditions = append(conditions, condition)
	}

	if len(conditions) > 0 {
		return " WHERE " + strings.Join(conditions, " AND "), args
	}
	return "", nil
}

func getNamespaceForTheGivenColumn(k string, mp map[string]string) string {
	if mp == nil {
		return ""
	}

	if mp[k] != "" {
		return mp[k] + "."
	}

	return ""
}

func (pag *Pagination) CreateResult(totalItems uint) PaginationResult {
	if pag == nil {
		return PaginationResult{}
	}
	return PaginationResult{
		Page:       pag.Page,
		PerPage:    pag.PerPage,
		TotalItems: totalItems,
		TotalPages: uint(math.Ceil(float64(totalItems) / float64(pag.PerPage))),
	}
}

func (sorting *Sorting) Validate(sort *string) bool {
	if sorting == nil {
		return false
	}

	_, ok := sorting.AllowedSorts[*sort]
	return ok
}

func (filters *Filtering) Validate(filter Filter, isRawQuery bool) bool {
	if filters == nil || len(filters.Filters) == 0 {
		return false
	}

	found := false
	for allowedFilter := range filters.AllowedFilters {
		if isRawQuery == true {
			allowedFilter = strings.Split(allowedFilter, " ")[0]
			if filter.Attribute == allowedFilter {
				found = true
				break
			}
		}
		if filter.Attribute == allowedFilter {
			found = true
			break
		}
	}

	return found
}

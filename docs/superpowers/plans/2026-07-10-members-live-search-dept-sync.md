# Members Live Search via dept-sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move members-page search from Multica's whole-tree fetch + in-memory filter to a first-class real-time search endpoint in costrict-dept-sync, and switch Multica's client to call it.

**Architecture:** Two new read-only Gin endpoints in costrict-dept-sync (`GET /v1/users/search`, `GET /v1/departments/search`) do DB-level `LOWER(col) LIKE LOWER(?)` matching (Postgres + SQLite portable), one row per person. Multica's `deptsync.Client.SearchUsers/SearchDepartments` drop the N+1 tree fan-out and call these endpoints directly. Multica's proxy handlers, frontend, and add→upsert path are unchanged.

**Tech Stack:** Go 1.25 (costrict-dept-sync, Gin + GORM), Go 1.26 (multica, Chi). SQLite `:memory:` for tests. See spec: [docs/superpowers/specs/2026-07-10-members-live-search-dept-sync-design.md](../specs/2026-07-10-members-live-search-dept-sync-design.md).

**Two repos:** Phase A lands in `e:\Projects\costrict-dept-sync`. Phase B lands in `e:\Projects\multica`. **Deploy costrict-dept-sync first** — Multica only works against a dept-sync that has the new endpoints.

---

## File Structure

**costrict-dept-sync** (`e:\Projects\costrict-dept-sync`):
- Modify: `pkg/repository/user_dept.go` — add `SearchByKeyword`.
- Modify: `pkg/repository/department.go` — add `SearchByKeyword`.
- Modify: `pkg/service/query.go` — add `SearchUsers`, `SearchDepartments`; add `"strings"` import.
- Modify: `pkg/http/handler/dept.go` — add `SearchUsers`, `SearchDepartments`, `parseSearchLimit`; add `strconv`,`strings` imports.
- Modify: `pkg/http/router.go` — register two routes.
- Create: `pkg/service/query_test.go` — service-layer tests (reuses `setupTestDB`).
- Create: `pkg/http/handler/search_test.go` — handler tests.

**multica** (`e:\Projects\multica`):
- Modify: `server/internal/deptsync/client.go` — rewrite `SearchUsers`/`SearchDepartments`; add `strconv` import; delete 4 dead helpers.
- Modify: `server/internal/deptsync/client_test.go` — rewrite the 3 Search* tests.

---

## Phase A — costrict-dept-sync

### Task A1: Service-layer tests for search (failing first)

**Files:**
- Create: `pkg/service/query_test.go`

This file lives in `package service`, so it reuses `setupTestDB`/`teardownTestDB` already defined in `pkg/service/sync_test.go`.

- [ ] **Step 1: Write the failing tests**

Create `e:\Projects\costrict-dept-sync\pkg\service\query_test.go`:

```go
package service

import (
	"testing"

	"costrict-dept-sync/pkg/db"
	"costrict-dept-sync/pkg/model"
)

// ptrStr returns a pointer to s, for seeding nullable string columns.
func ptrStr(s string) *string { return &s }

// seedSearchData inserts a small, self-consistent org for search tests:
//   departments: D001 技术部, D002 产品部, D003 前端组 (child of D001)
//   users:
//     U001 张三  zhangsan@example.com  D001(main) + D003   — multi-dept, tests dedup
//     U002 Li Si lisi@example.com      D002(main)          — tests case-insensitive email
//     U003 王五  (no universal_id)     D002(main)          — tests user_id fallback
//     U004 赵六  disabled@example.com  D002                — status=0, must be filtered out
func seedSearchData(t *testing.T) {
	t.Helper()
	depts := []model.Department{
		{DeptID: "D001", DeptName: "技术部", DeptPath: ptrStr("/D001"), DeptLevel: 1, OrderNum: 1, Status: 1},
		{DeptID: "D002", DeptName: "产品部", DeptPath: ptrStr("/D002"), DeptLevel: 1, OrderNum: 2, Status: 1},
		{DeptID: "D003", DeptName: "前端组", DeptPath: ptrStr("/D001/D003"), DeptLevel: 2, OrderNum: 1, Status: 1},
	}
	if err := db.DB.Create(&depts).Error; err != nil {
		t.Fatalf("seed departments: %v", err)
	}
	users := []model.UserDepartment{
		{UserID: "U001", Username: "张三", UniversalID: "zhangsan@example.com", DeptID: "D001", IsMain: 1, Position: ptrStr("工程师"), Status: 1},
		{UserID: "U001", Username: "张三", UniversalID: "zhangsan@example.com", DeptID: "D003", IsMain: 0, Position: ptrStr("工程师"), Status: 1},
		{UserID: "U002", Username: "Li Si", UniversalID: "lisi@example.com", DeptID: "D002", IsMain: 1, Position: ptrStr("经理"), Status: 1},
		{UserID: "U003", Username: "王五", UniversalID: "", DeptID: "D002", IsMain: 1, Position: ptrStr("产品经理"), Status: 1},
		{UserID: "U004", Username: "赵六", UniversalID: "disabled@example.com", DeptID: "D002", IsMain: 1, Position: ptrStr("离职"), Status: 0},
	}
	if err := db.DB.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
}

func TestQueryService_SearchUsers_ByChineseName(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	users, err := svc.SearchUsers("张三", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d: %+v", len(users), users)
	}
	if users[0].UserID != "U001" || users[0].Username != "张三" {
		t.Fatalf("unexpected user: %+v", users[0])
	}
	// Dedup keeps the main-department row (is_main=1) → 技术部, not 前端组.
	if users[0].DeptID != "D001" || users[0].DeptName != "技术部" {
		t.Fatalf("expected main dept 技术部 (D001), got dept_id=%s name=%q", users[0].DeptID, users[0].DeptName)
	}
}

func TestQueryService_SearchUsers_DedupesMultiDept(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	// Email matches both of U001's rows; expect a single result on the main dept.
	users, err := svc.SearchUsers("zhangsan", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 deduped user, got %d: %+v", len(users), users)
	}
	if users[0].DeptID != "D001" {
		t.Fatalf("expected main dept D001, got %s", users[0].DeptID)
	}
}

func TestQueryService_SearchUsers_CaseInsensitive(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	// Stored email is lowercase; search with uppercase.
	users, err := svc.SearchUsers("LISI@EXAMPLE.COM", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != "U002" {
		t.Fatalf("expected case-insensitive match for U002, got %+v", users)
	}
}

func TestQueryService_SearchUsers_FiltersInactive(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	users, err := svc.SearchUsers("disabled", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no inactive (status=0) users, got %+v", users)
	}
}

func TestQueryService_SearchUsers_EmptyKeywordReturnsEmpty(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	users, err := svc.SearchUsers("   ", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected empty result for blank keyword, got %+v", users)
	}
}

func TestQueryService_SearchUsers_ByEmployeeIDWithoutUniversalID(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	// U003 has no universal_id; must still be found by user_id.
	users, err := svc.SearchUsers("U003", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != "U003" {
		t.Fatalf("expected U003 by employee id, got %+v", users)
	}
}

func TestQueryService_SearchDepartments_ByName(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	depts, err := svc.SearchDepartments("前端", 20)
	if err != nil {
		t.Fatalf("SearchDepartments: %v", err)
	}
	if len(depts) != 1 || depts[0].DeptID != "D003" || depts[0].DeptName != "前端组" {
		t.Fatalf("expected D003 前端组, got %+v", depts)
	}
}

func TestQueryService_SearchDepartments_EmptyKeywordReturnsEmpty(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()
	seedSearchData(t)

	svc := NewQueryService(nil, 0)
	depts, err := svc.SearchDepartments("", 20)
	if err != nil {
		t.Fatalf("SearchDepartments: %v", err)
	}
	if len(depts) != 0 {
		t.Fatalf("expected empty result, got %+v", depts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd e:/Projects/costrict-dept-sync && go test ./pkg/service/ -run TestQueryService_Search -v`
Expected: FAIL / build error — `svc.SearchUsers undefined` and `svc.SearchDepartments undefined` (type `QueryService` has no such methods).

### Task A2: Implement repo + service search methods

**Files:**
- Modify: `pkg/repository/user_dept.go` — add `SearchByKeyword` (insert after `FindByUniversalID`, before `DeleteByVersion`).
- Modify: `pkg/repository/department.go` — add `SearchByKeyword` (insert after `FindAll`, before `FindByVersion`).
- Modify: `pkg/service/query.go` — add `SearchUsers` + `SearchDepartments` (insert after `GetDeptUsers`); add `"strings"` to imports.

- [ ] **Step 1: Add `UserDeptRepo.SearchByKeyword`**

In `e:\Projects\costrict-dept-sync\pkg\repository\user_dept.go`, insert after the `FindByUniversalID` method (after line 98):

```go
// SearchByKeyword 关键字实时搜索用户部门关联（姓名/邮箱/工号/职位）。
// 大小写不敏感子串匹配，仅返回 status=1 的记录；按 is_main 降序、username 升序排列，
// 便于上层按人去重时优先保留主部门记录。
func (repo *UserDeptRepo) SearchByKeyword(keyword string, limit int) ([]model.UserDepartment, error) {
	if keyword == "" {
		return nil, nil
	}
	like := "%" + keyword + "%"
	var records []model.UserDepartment
	err := db.DB.Where(
		"status = 1 AND (LOWER(username) LIKE LOWER(?) OR LOWER(universal_id) LIKE LOWER(?) OR LOWER(user_id) LIKE LOWER(?) OR LOWER(position) LIKE LOWER(?))",
		like, like, like, like,
	).Order("is_main DESC, username").Limit(limit).Find(&records).Error
	return records, err
}
```

- [ ] **Step 2: Add `DepartmentRepo.SearchByKeyword`**

In `e:\Projects\costrict-dept-sync\pkg\repository\department.go`, insert after the `FindAll` method (after line 78):

```go
// SearchByKeyword 关键字实时搜索部门（名称/路径/ID）。
// 大小写不敏感子串匹配，仅返回 status=1 的记录。
func (repo *DepartmentRepo) SearchByKeyword(keyword string, limit int) ([]model.Department, error) {
	if keyword == "" {
		return nil, nil
	}
	like := "%" + keyword + "%"
	var depts []model.Department
	err := db.DB.Where(
		"status = 1 AND (LOWER(dept_name) LIKE LOWER(?) OR LOWER(dept_path) LIKE LOWER(?) OR LOWER(dept_id) LIKE LOWER(?))",
		like, like, like,
	).Order("order_num, dept_level, dept_id").Limit(limit).Find(&depts).Error
	return depts, err
}
```

- [ ] **Step 3: Add `"strings"` to query.go imports**

In `e:\Projects\costrict-dept-sync\pkg\service\query.go`, the import block is lines 3-13. Add `"strings"` so it reads:

```go
import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"costrict-dept-sync/pkg/model"
	"costrict-dept-sync/pkg/repository"
	"costrict-dept-sync/pkg/util/cache"

	"go.uber.org/zap"
)
```

- [ ] **Step 4: Add `QueryService.SearchUsers` and `SearchDepartments`**

In `e:\Projects\costrict-dept-sync\pkg\service\query.go`, insert after the `GetDeptUsers` method (after line 449, before `collectDeptIDs`):

```go
// SearchUsers 关键字实时搜索用户（姓名/邮箱/工号/职位），每人仅返回一条记录（优先主部门）。
// 不走缓存，保证结果反映最新同步数据。
func (s *QueryService) SearchUsers(keyword string, limit int) ([]*model.DeptUserInfo, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []*model.DeptUserInfo{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	// Over-fetch to compensate for per-person dedup (one person may occupy several rows),
	// capped to avoid large scans.
	fetchLimit := limit * 3
	if fetchLimit > 150 {
		fetchLimit = 150
	}

	records, err := s.userDeptRepo.SearchByKeyword(keyword, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("搜索用户失败: %w", err)
	}

	deptCache := make(map[string]*model.Department)
	seen := make(map[string]struct{})
	var result []*model.DeptUserInfo
	for _, r := range records {
		key := strings.TrimSpace(r.UniversalID)
		if key == "" {
			key = strings.TrimSpace(r.UserID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		// deptCache stores nil for missing depts too, so we don't re-query them.
		dept, cached := deptCache[r.DeptID]
		if !cached {
			d, e := s.deptRepo.FindByDeptID(r.DeptID)
			if e == nil {
				dept = d // may be nil if the department row is absent
			}
			deptCache[r.DeptID] = dept
		}

		uid := r.UniversalID
		info := &model.DeptUserInfo{
			UserID:      r.UserID,
			Username:    r.Username,
			UniversalID: &uid,
			DeptID:      r.DeptID,
			IsMain:      r.IsMain,
			Position:    r.Position,
			Status:      r.Status,
		}
		if dept != nil {
			info.DeptName = dept.DeptName
			info.DeptPath = dept.DeptPath
		}
		result = append(result, info)

		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

// SearchDepartments 关键字实时搜索部门（名称/路径/ID）。不走缓存。
func (s *QueryService) SearchDepartments(keyword string, limit int) ([]model.Department, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []model.Department{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	depts, err := s.deptRepo.SearchByKeyword(keyword, limit)
	if err != nil {
		return nil, fmt.Errorf("搜索部门失败: %w", err)
	}
	return depts, nil
}
```

- [ ] **Step 5: Run the service tests to verify they pass**

Run: `cd e:/Projects/costrict-dept-sync && go test ./pkg/service/ -run TestQueryService_Search -v`
Expected: PASS — all 8 tests green.

- [ ] **Step 6: Commit**

```bash
cd e:/Projects/costrict-dept-sync
git add pkg/repository/user_dept.go pkg/repository/department.go pkg/service/query.go pkg/service/query_test.go
git commit -m "feat(search): add real-time user/department keyword search service"
```

### Task A3: Handler tests for search (failing first)

**Files:**
- Create: `pkg/http/handler/search_test.go`

Uses GORM `AutoMigrate` to stand up `:memory:` tables (no DDL duplication) and `gin.CreateTestContext` to drive the handler directly.

- [ ] **Step 1: Write the failing handler tests**

Create `e:\Projects\costrict-dept-sync\pkg\http\handler\search_test.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"costrict-dept-sync/pkg/db"
	"costrict-dept-sync/pkg/model"
	"costrict-dept-sync/pkg/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupSearchHandlerDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := testDB.AutoMigrate(&model.Department{}, &model.UserDepartment{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.DB = testDB
}

func searchStrPtr(s string) *string { return &s }

func newSearchRequest(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func TestDeptHandler_SearchUsers(t *testing.T) {
	setupSearchHandlerDB(t)
	if err := db.DB.Create(&[]model.Department{
		{DeptID: "D1", DeptName: "Engineering", DeptPath: searchStrPtr("/D1"), Status: 1},
	}).Error; err != nil {
		t.Fatalf("seed dept: %v", err)
	}
	if err := db.DB.Create(&[]model.UserDepartment{
		{UserID: "U1", Username: "Alice", UniversalID: "alice@example.com", DeptID: "D1", IsMain: 1, Position: searchStrPtr("Engineer"), Status: 1},
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	h := NewDeptHandler(service.NewQueryService(nil, 0))
	c, w := newSearchRequest(http.MethodGet, "/v1/users/search?q=alice&limit=10")
	h.SearchUsers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Success bool                 `json:"success"`
		Data    []model.DeptUserInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if !envelope.Success || len(envelope.Data) != 1 || envelope.Data[0].UserID != "U1" {
		t.Fatalf("unexpected: success=%v data=%+v body=%s", envelope.Success, envelope.Data, w.Body.String())
	}
}

func TestDeptHandler_SearchUsers_EmptyQReturnsEmptyArray(t *testing.T) {
	setupSearchHandlerDB(t)
	h := NewDeptHandler(service.NewQueryService(nil, 0))
	c, w := newSearchRequest(http.MethodGet, "/v1/users/search?q=")
	h.SearchUsers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Success bool                 `json:"success"`
		Data    []model.DeptUserInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !envelope.Success || len(envelope.Data) != 0 {
		t.Fatalf("expected success with empty array, body=%s", w.Body.String())
	}
}

func TestDeptHandler_SearchDepartments(t *testing.T) {
	setupSearchHandlerDB(t)
	if err := db.DB.Create(&[]model.Department{
		{DeptID: "D1", DeptName: "Platform", DeptPath: searchStrPtr("/D1"), Status: 1},
	}).Error; err != nil {
		t.Fatalf("seed dept: %v", err)
	}

	h := NewDeptHandler(service.NewQueryService(nil, 0))
	c, w := newSearchRequest(http.MethodGet, "/v1/departments/search?q=plat")
	h.SearchDepartments(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Success bool               `json:"success"`
		Data    []model.Department `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if !envelope.Success || len(envelope.Data) != 1 || envelope.Data[0].DeptID != "D1" {
		t.Fatalf("unexpected: success=%v data=%+v", envelope.Success, envelope.Data)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd e:/Projects/costrict-dept-sync && go test ./pkg/http/handler/ -run TestDeptHandler_Search -v`
Expected: FAIL / build error — `h.SearchUsers undefined`, `h.SearchDepartments undefined`.

### Task A4: Implement handlers + register routes

**Files:**
- Modify: `pkg/http/handler/dept.go` — add imports `strconv`,`strings`; add `SearchUsers`, `SearchDepartments`, `parseSearchLimit` (append after `GetDeptUsers`, end of file).
- Modify: `pkg/http/router.go` — register two routes inside the v1 group.

- [ ] **Step 1: Add `strconv` and `strings` imports to dept.go**

In `e:\Projects\costrict-dept-sync\pkg\http\handler\dept.go`, the import block is lines 4-10. Replace it with:

```go
import (
	"strconv"
	"strings"

	"costrict-dept-sync/pkg/http/resp"
	"costrict-dept-sync/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)
```

- [ ] **Step 2: Append the two handlers and the limit helper**

Append to the end of `e:\Projects\costrict-dept-sync\pkg\http\handler\dept.go` (after `GetDeptUsers`):

```go
// SearchUsers 关键字实时搜索用户。
// @Summary      搜索用户
// @Description  按关键字（姓名/邮箱/工号/职位）实时搜索用户，每人返回主部门一条记录。
// @Tags         部门查询
// @Accept       json
// @Produce      json
// @Param        q     query   string  true  "搜索关键字" example("张三")
// @Param        limit query   int     false "返回数量，默认 20，最大 50" example(20)
// @Success      200  {object}  resp.SuccessResponse{data=[]DeptUserInfo}  "成功返回用户列表"
// @Failure      400  {object}  resp.ErrorResponse                      "请求失败"
// @Router       /v1/users/search [get]
// @Security     QueryKeyAuth
func (h *DeptHandler) SearchUsers(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("q"))
	limit := parseSearchLimit(c.Query("limit"))

	users, err := h.queryService.SearchUsers(keyword, limit)
	if err != nil {
		zap.L().Error("搜索用户失败", zap.Error(err), zap.String("q", keyword))
		resp.ErrorMsg(c, "search_users_error", "搜索用户失败: "+err.Error())
		return
	}
	if users == nil {
		resp.Success(c, []interface{}{})
		return
	}
	resp.Success(c, users)
}

// SearchDepartments 关键字实时搜索部门。
// @Summary      搜索部门
// @Description  按关键字（名称/路径/ID）实时搜索部门。
// @Tags         部门查询
// @Accept       json
// @Produce      json
// @Param        q     query   string  true  "搜索关键字" example("研发")
// @Param        limit query   int     false "返回数量，默认 20，最大 50" example(20)
// @Success      200  {object}  resp.SuccessResponse{data=[]Department}  "成功返回部门列表"
// @Failure      400  {object}  resp.ErrorResponse                   "请求失败"
// @Router       /v1/departments/search [get]
// @Security     QueryKeyAuth
func (h *DeptHandler) SearchDepartments(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("q"))
	limit := parseSearchLimit(c.Query("limit"))

	depts, err := h.queryService.SearchDepartments(keyword, limit)
	if err != nil {
		zap.L().Error("搜索部门失败", zap.Error(err), zap.String("q", keyword))
		resp.ErrorMsg(c, "search_departments_error", "搜索部门失败: "+err.Error())
		return
	}
	if depts == nil {
		resp.Success(c, []interface{}{})
		return
	}
	resp.Success(c, depts)
}

// parseSearchLimit 解析搜索接口的 limit 参数：缺省 20，非正数回退默认值，上限 50。
func parseSearchLimit(raw string) int {
	const defaultLimit = 20
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > 50 {
		return 50
	}
	return n
}
```

- [ ] **Step 3: Register the two routes**

In `e:\Projects\costrict-dept-sync\pkg\http\router.go`, inside the v1 group `if deps.DeptHandler != nil && deps.QueryKeyRepo != nil { ... }` block (after line 162 — the `user/:user_id/departments` route — and before the closing brace on line 163), add:

```go
		// 关键字实时搜索用户（每人返回主部门一条）
		apiRouter.GET("/users/search", deps.DeptHandler.SearchUsers)
		// 关键字实时搜索部门
		apiRouter.GET("/departments/search", deps.DeptHandler.SearchDepartments)
```

- [ ] **Step 4: Run handler tests to verify they pass**

Run: `cd e:/Projects/costrict-dept-sync && go test ./pkg/http/handler/ -run TestDeptHandler_Search -v`
Expected: PASS — all 3 handler tests green.

- [ ] **Step 5: Build the whole module**

Run: `cd e:/Projects/costrict-dept-sync && go build ./... && go vet ./...`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
cd e:/Projects/costrict-dept-sync
git add pkg/http/handler/dept.go pkg/http/handler/search_test.go pkg/http/router.go
git commit -m "feat(search): expose /v1/users/search and /v1/departments/search endpoints"
```

---

## Phase B — multica

### Task B1: Rewrite deptsync client tests to expect the live endpoints (failing first)

**Files:**
- Modify: `server/internal/deptsync/client_test.go` — replace the 3 Search* tests.

The existing tests assert the OLD tree-fetch behavior. After the rewrite they must assert the NEW single-endpoint behavior. `TestClientListDepartmentUsers*` and `TestClientGetDepartment*` are unchanged.

- [ ] **Step 1: Replace the three Search* tests**

In `e:\Projects\multica\server\internal\deptsync\client_test.go`, delete these three functions in full:
- `TestClientSearchDepartmentsUsesTreeAndQueryKey` (lines ~111-174)
- `TestClientSearchDepartmentsToleratesEmptyStringChildren` (lines ~176-208)
- `TestClientSearchUsersUsesDepartmentTreeAndQueryKey` (lines ~210-317)

and replace them with:

```go
func TestClientSearchUsersCallsSearchEndpoint(t *testing.T) {
	var sawQueryKey string
	var sawPath string
	var sawQ string
	var sawLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.Header.Get("X-Query-Key")
		sawPath = r.URL.Path
		sawQ = r.URL.Query().Get("q")
		sawLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": "",
			"data": [
				{
					"user_id": "E001",
					"username": "Alice Platform",
					"universal_id": "u-1",
					"dept_id": "D110",
					"dept_name": "客户成功组",
					"is_main": 1,
					"position": "Engineer",
					"status": 1,
					"dept_path": "研发体系/Costrict研发部/客户成功组"
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	users, err := client.SearchUsers(t.Context(), "E001", 20)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if sawQueryKey != "secret" {
		t.Fatalf("expected X-Query-Key header, got %q", sawQueryKey)
	}
	if sawPath != "/users/search" {
		t.Fatalf("unexpected path %q (want /users/search, no tree fetch)", sawPath)
	}
	if sawQ != "E001" {
		t.Fatalf("unexpected q %q", sawQ)
	}
	if sawLimit != "20" {
		t.Fatalf("unexpected limit %q", sawLimit)
	}
	if len(users) != 1 || users[0].UserID != "E001" || users[0].Username != "Alice Platform" {
		t.Fatalf("unexpected users: %+v", users)
	}
	// dept_path now comes straight from the server; no client-side rebuild.
	if users[0].DeptPath != "研发体系/Costrict研发部/客户成功组" {
		t.Fatalf("expected server-provided dept_path, got %q", users[0].DeptPath)
	}
}

func TestClientSearchUsersClampsLimit(t *testing.T) {
	var sawLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": []}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	if _, err := client.SearchUsers(t.Context(), "x", 0); err != nil {
		t.Fatalf("SearchUsers limit=0: %v", err)
	}
	if sawLimit != "20" {
		t.Fatalf("expected clamped limit 20 for zero, got %q", sawLimit)
	}
	if _, err := client.SearchUsers(t.Context(), "x", 999); err != nil {
		t.Fatalf("SearchUsers limit=999: %v", err)
	}
	if sawLimit != "20" {
		t.Fatalf("expected clamped limit 20 for over-max, got %q", sawLimit)
	}
}

func TestClientSearchDepartmentsCallsSearchEndpoint(t *testing.T) {
	var sawQueryKey string
	var sawPath string
	var sawQ string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.Header.Get("X-Query-Key")
		sawPath = r.URL.Path
		sawQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"dept_id": "D100",
					"dept_name": "Platform Dept",
					"dept_path": "/D000/D010/D100",
					"parent_dept_id": "D010",
					"dept_level": 3,
					"child_dept_count": 1
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, QueryKey: "secret"})
	departments, err := client.SearchDepartments(t.Context(), "plat", 10)
	if err != nil {
		t.Fatalf("SearchDepartments: %v", err)
	}
	if sawQueryKey != "secret" {
		t.Fatalf("expected X-Query-Key header, got %q", sawQueryKey)
	}
	if sawPath != "/departments/search" {
		t.Fatalf("unexpected path %q (want /departments/search)", sawPath)
	}
	if sawQ != "plat" {
		t.Fatalf("unexpected q %q", sawQ)
	}
	if len(departments) != 1 || departments[0].DeptID != "D100" || departments[0].DeptName != "Platform Dept" {
		t.Fatalf("unexpected departments: %+v", departments)
	}
	if departments[0].DeptPath != "/D000/D010/D100" {
		t.Fatalf("expected server-provided dept_path, got %q", departments[0].DeptPath)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd e:/Projects/multica/server && go test ./internal/deptsync/ -run TestClientSearch -v`
Expected: FAIL — the mock servers only handle `/users/search` / `/departments/search` but `client.SearchUsers` still fetches `/department/tree` first, so requests hit the default `http.NotFound` path and return empty results (e.g. "unexpected path" / "expected one user, got 0").

### Task B2: Rewrite the client to call the live endpoints + remove dead code

**Files:**
- Modify: `server/internal/deptsync/client.go` — add `"strconv"` import; rewrite `SearchDepartments` (lines 151-175) and `SearchUsers` (lines 177-219); delete four now-dead helpers.

- [ ] **Step 1: Add `strconv` to imports**

In `e:\Projects\multica\server\internal\deptsync\client.go`, the import block is lines 3-12. Add `"strconv"`:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)
```

- [ ] **Step 2: Rewrite `SearchDepartments`**

Replace the entire `SearchDepartments` function (current lines 151-175) with:

```go
func (c *Client) SearchDepartments(ctx context.Context, query string, limit int) ([]Department, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	u, err := url.Parse(c.baseURL + "/departments/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept department search request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool         `json:"success"`
		Code    string       `json:"code"`
		Data    []Department `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}
```

- [ ] **Step 3: Rewrite `SearchUsers`**

Replace the entire `SearchUsers` function (current lines 177-219) with:

```go
func (c *Client) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	u, err := url.Parse(c.baseURL + "/users/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept user search request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}
```

- [ ] **Step 4: Delete the four now-dead helpers**

In `e:\Projects\multica\server\internal\deptsync\client.go`, delete these four functions entirely (they were only used by the old in-memory search path; `listDepartmentTree`, `findDepartment`, `displayDeptPathByID`, `applyDisplayDeptPaths` are still used by `GetDepartment`/`ListDepartmentUsers` and stay):

- `flattenDepartments` (current lines ~278-293)
- `applyDisplayDepartmentPaths` (current lines ~323-329)
- `departmentMatches` (current lines ~331-335)
- `userMatches` (current lines ~337-340)

After deletion, `displayDeptPathByID` and `applyDisplayDeptPaths` remain (used by `ListDepartmentUsers` at lines ~355-366).

- [ ] **Step 5: Run the deptsync tests to verify they pass**

Run: `cd e:/Projects/multica/server && go test ./internal/deptsync/ -v`
Expected: PASS — all tests green, including the rewritten Search* tests and the unchanged List/Get tests.

- [ ] **Step 6: Verify build + vet**

Run: `cd e:/Projects/multica/server && go build ./... && go vet ./...`
Expected: no output (success). Confirms no remaining references to the deleted helpers and `strconv` is used.

- [ ] **Step 7: Commit**

```bash
cd e:/Projects/multica
git add server/internal/deptsync/client.go server/internal/deptsync/client_test.go
git commit -m "refactor(deptsync): search dept-sync live instead of fetching whole tree"
```

---

## Phase C — verification

### Task C1: Full test suites + smoke test

- [ ] **Step 1: costrict-dept-sync full tests**

Run: `cd e:/Projects/costrict-dept-sync && go test ./...`
Expected: PASS.

- [ ] **Step 2: multica full Go tests**

Run: `cd e:/Projects/multica/server && go test ./...`
Expected: PASS (no regression in handler/workspace_dept tests that exercise `SearchUsers`).

- [ ] **Step 3: Smoke test the new endpoints (manual, requires running costrict-dept-sync with a query key)**

With costrict-dept-sync running and a valid `X-Query-Key`:

```bash
curl -s -H "X-Query-Key: $DEPT_SYNC_QUERY_KEY" \
  "http://127.0.0.1:8080/costrict-dept-info/api/v1/users/search?q=zhang&limit=5" | jq
curl -s -H "X-Query-Key: $DEPT_SYNC_QUERY_KEY" \
  "http://127.0.0.1:8080/costrict-dept-info/api/v1/departments/search?q=技术&limit=5" | jq
```

Expected: `{"success":true,"code":"","data":[ ... ]}` with matched users / departments; empty `q` returns `"data":[]`.

- [ ] **Step 4: End-to-end in Multica (manual)**

Start both services, open a dept-backed workspace → members page → admin "add member from department directory" box → type a name/email. Expected: live results from costrict-dept-sync, one row per person; selecting and adding still upserts into `multica_member` (unchanged).

---

## Notes

- **No frontend changes.** `packages/views/settings/components/members-tab.tsx`, the core API client, and the multica proxy handlers (`server/internal/handler/dept.go`) keep their `q`/`limit` contract and response shapes. The 200ms debounce already gives a real-time feel.
- **Batch-add benefits for free.** `BatchAddDeptMembers` resolves refs through `SearchUsers`, so it inherits the faster single-call path with no extra change.
- **Cross-dialect case-insensitivity** is handled by `LOWER(col) LIKE LOWER(?)` (works on Postgres and SQLite). Chinese names are unaffected by case.
- **`dept_path` format:** search results return the `dept_path` stored by costrict-dept-sync (no client-side rebuild). Accepted per spec.
- **Out of scope (per spec):** pinyin search; removing the dead `GET .../members/search` endpoint; redesigning `/dept-sync`; pagination beyond the `limit` clamp.

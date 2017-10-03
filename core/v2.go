package core

import (
	"regexp"
	"strings"

	"github.com/pborman/uuid"
	"github.com/starkandwayne/goutils/log"

	"github.com/starkandwayne/shield/db"
	"github.com/starkandwayne/shield/route"
	"github.com/starkandwayne/shield/util"
)

type v2AuthProvider struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
}

type v2AuthProviderFull struct {
	Name       string                 `json:"name"`
	Identifier string                 `json:"identifier"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

type v2SystemArchive struct {
	UUID     uuid.UUID `json:"uuid"`
	Schedule string    `json:"schedule"`
	TakenAt  int64     `json:"taken_at"`
	Expiry   int       `json:"expiry"`
	Size     int       `json:"size"`
	OK       bool      `json:"ok"`
	Notes    string    `json:"notes"`
}
type v2SystemTask struct {
	UUID      uuid.UUID        `json:"uuid"`
	Type      string           `json:"type"`
	Status    string           `json:"status"`
	Owner     string           `json:"owner"`
	StartedAt int64            `json:"started_at"`
	OK        bool             `json:"ok"`
	Notes     string           `json:"notes"`
	Archive   *v2SystemArchive `json:"archive,omitempty"`
}
type v2SystemJob struct {
	UUID     uuid.UUID `json:"uuid"`
	Schedule string    `json:"schedule"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	OK       bool      `json:"ok"`

	Store struct {
		UUID    uuid.UUID `json:"uuid"`
		Name    string    `json:"name"`
		Summary string    `json:"summary"`
		Plugin  string    `json:"plugin"`
	} `json:"store"`

	Keep struct {
		N    int `json:"n"`
		Days int `json:"days"`
	} `json:"keep"`

	Retention struct {
		UUID    uuid.UUID `json:"uuid"`
		Name    string    `json:"name"`
		Summary string    `json:"summary"`
		Days    int       `json:"days"`
	} `json:"retention"`
}
type v2System struct {
	UUID  uuid.UUID `json:"uuid"`
	Name  string    `json:"name"`
	Notes string    `json:"notes"`
	OK    bool      `json:"ok"`

	Jobs  []v2SystemJob  `json:"jobs"`
	Tasks []v2SystemTask `json:"tasks"`
}

type v2PatchAnnotation struct {
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	Disposition string `json:"disposition"`
	Notes       string `json:"notes"`
	Clear       string `json:"clear"`
}

func (core *Core) v2API() *route.Router {
	r := &route.Router{}

	r.Dispatch("GET /v2/health", func(r *route.Request) { // {{{
		health, err := core.checkHealth()
		if err != nil {
			r.Fail(route.Oops(err, "failed to check SHIELD health"))
			return
		}
		r.OK(health)
	})
	// }}}

	r.Dispatch("POST /v2/init", func(r *route.Request) { // {{{
		var in struct {
			Master string `json:"master_password"`
		}
		if !r.Payload(&in) {
			return
		}

		/* FIXME: need a better way of doing Missing Parameters */
		e := MissingParameters()
		e.Check("master_password", in.Master)
		if e.IsValid() {
			r.Fail(route.Bad(e, "%s", e))
			return
		}

		init, err := core.Initialize(in.Master)
		if err != nil {
			r.Fail(route.Oops(err, "failed to initialize the SHIELD core"))
			return
		}
		if !init {
			r.Fail(route.Bad(nil, "this SHIELD core has already been initialized"))
			return
		}

		r.Success("Successfully initialzied the SHIELD core")
	})
	// }}}
	r.Dispatch("POST /v2/unlock", func(r *route.Request) { // {{{
		var in struct {
			Master string `json:"master_password"`
		}
		if !r.Payload(&in) {
			return
		}

		/* FIXME: need a better way of doing Missing Parameters */
		e := MissingParameters()
		e.Check("master_password", in.Master)
		if e.IsValid() {
			r.Fail(route.Bad(e, "%s", e))
			return
		}

		init, err := core.Unlock(in.Master)
		if err != nil {
			r.Fail(route.Oops(err, "failed to unlock the SHIELD core"))
			return
		}
		if !init {
			r.Fail(route.Bad(nil, "this SHIELD core has not yet been initialized"))
			return
		}

		r.Success("Successfully unlocked the SHIELD core")
	})
	// }}}
	r.Dispatch("POST /v2/rekey", func(r *route.Request) { // {{{
		var in struct {
			CurMaster string `json:"current_master_password"`
			NewMaster string `json:"new_master_password"`
		}
		if !r.Payload(&in) {
			return
		}

		/* FIXME: need a better way of doing Missing Parameters */
		e := MissingParameters()
		e.Check("current_master_password", in.CurMaster)
		e.Check("new_master_password", in.NewMaster)
		if e.IsValid() {
			r.Fail(route.Bad(e, "%s", e))
			return
		}

		err := core.Rekey(in.CurMaster, in.NewMaster)
		if err != nil {
			r.Fail(route.Oops(err, "failed to rekey the SHIELD core"))
			return
		}

		r.Success("Successfully rekeyed the SHIELD core")
	})
	// }}}

	r.Dispatch("GET /v2/auth/providers", func(r *route.Request) { // {{{
		l := make([]v2AuthProvider, 0)
		for _, auth := range core.auth {
			l = append(l, v2AuthProvider{
				Name:       auth.Name,
				Identifier: auth.Identifier,
				Type:       auth.Backend,
			})
		}
		r.OK(l)
	})
	// }}}
	r.Dispatch("GET /v2/auth/provider/:name", func(r *route.Request) { // {{{
		for _, a := range core.auth {
			if a.Identifier == r.Args[1] {
				r.OK(&v2AuthProviderFull{
					Name:       a.Name,
					Identifier: a.Identifier,
					Type:       a.Backend,
					Properties: util.StringifyKeys(a.Properties).(map[string]interface{}),
				})
				return
			}
		}
		r.Fail(route.NotFound(nil, "no such authentication provider: '%s'", r.Args[1]))
	})
	// }}}

	r.Dispatch("GET /v2/systems", func(r *route.Request) { // {{{
		targets, err := core.DB.GetAllTargets(
			&db.TargetFilter{
				SkipUsed:   r.ParamIs("unused", "t"),
				SkipUnused: r.ParamIs("unused", "f"),
				SearchName: r.Param("name", ""),
				ForPlugin:  r.Param("plugin", ""),
				ExactMatch: r.ParamIs("exact", "t"),
			},
		)
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve systems information"))
			return
		}

		systems := make([]v2System, len(targets))
		for i, target := range targets {
			err := core.v2copyTarget(&systems[i], target)
			if err != nil {
				r.Fail(route.Oops(err, "failed to retrieve systems information"))
				return
			}
		}

		r.OK(systems)
	})
	// }}}
	r.Dispatch("GET /v2/system/:uuid", func(r *route.Request) { // {{{
		log.Debugf("%s: got args [%v]", r, r.Args)
		target, err := core.DB.GetTarget(uuid.Parse(r.Args[1]))
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve system information"))
			return
		}

		if target == nil {
			r.Fail(route.NotFound(err, "system %s not found", r.Args[1]))
			return
		}

		var system v2System
		err = core.v2copyTarget(&system, target)
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve system information"))
			return
		}

		// keep track of our archives, indexed by task UUID
		archives := make(map[string]*db.Archive)
		aa, err := core.DB.GetAllArchives(
			&db.ArchiveFilter{
				ForTarget:  target.UUID.String(),
				WithStatus: []string{"valid"},
			},
		)
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve system information"))
			return
		}
		for _, archive := range aa {
			archives[archive.UUID.String()] = archive
		}

		tasks, err := core.DB.GetAllTasks(
			&db.TaskFilter{
				ForTarget:    target.UUID.String(),
				OnlyRelevant: true,
			},
		)
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve system information"))
			return
		}
		system.Tasks = make([]v2SystemTask, len(tasks))
		for i, task := range tasks {
			system.Tasks[i].UUID = task.UUID
			system.Tasks[i].Type = task.Op
			system.Tasks[i].Status = task.Status
			system.Tasks[i].Owner = task.Owner
			system.Tasks[i].OK = task.OK
			system.Tasks[i].Notes = task.Notes

			if t := task.StartedAt.Time(); t.IsZero() {
				system.Tasks[i].StartedAt = 0
			} else {
				system.Tasks[i].StartedAt = t.Unix()
			}

			if archive, ok := archives[task.ArchiveUUID.String()]; ok {
				system.Tasks[i].Archive = &v2SystemArchive{
					UUID:     archive.UUID,
					Schedule: archive.Job,
					Expiry:   (int)((archive.ExpiresAt.Time().Unix() - archive.TakenAt.Time().Unix()) / 86400),
					Notes:    archive.Notes,
					Size:     -1, // FIXME
				}
			}
		}

		r.OK(system)
	})
	// }}}
	r.Dispatch("POST /v2/systems", func(r *route.Request) { // {{{
		/* FIXME */
		r.Fail(route.Errorf(501, nil, "%s: not implemented", r))
	})
	// }}}
	r.Dispatch("PUT /v2/system/:uuid", func(r *route.Request) { // {{{
		/* FIXME */
		r.Fail(route.Errorf(501, nil, "%s: not implemented", r))
	})
	// }}}
	r.Dispatch("PATCH /v2/system/:uuid", func(r *route.Request) { // {{{
		var in struct {
			Annotations []v2PatchAnnotation `json:"annotations"`
		}
		if !r.Payload(&in) {
			return
		}

		target, err := core.DB.GetTarget(uuid.Parse(r.Args[1]))
		if err != nil {
			r.Fail(route.Bad(err, "invalid or malformed target UUID: '%s'", r.Args[1]))
			return
		}

		for _, ann := range in.Annotations {
			switch ann.Type {
			case "task":
				err = core.DB.AnnotateTargetTask(
					target.UUID,
					ann.UUID,
					&db.TaskAnnotation{
						Disposition: ann.Disposition,
						Notes:       ann.Notes,
						Clear:       ann.Clear,
					},
				)
				if err != nil {
					r.Fail(route.Oops(err, "failed to annotate task %s", ann.UUID))
					return
				}

			case "archive":
				err = core.DB.AnnotateTargetArchive(
					target.UUID,
					ann.UUID,
					ann.Notes,
				)
				if err != nil {
					r.Fail(route.Oops(err, "failed to annotate archive %s", ann.UUID))
					return
				}

			default:
				r.Fail(route.Bad(nil, "unrecognized system annotation type '%s'", ann.Type))
				return
			}
		}

		_ = core.DB.MarkTasksIrrelevant()
		r.Success("annotated successfully")
	})
	// }}}
	r.Dispatch("DELETE /v2/system/:uuid", func(r *route.Request) { // {{{
	})
	// }}}

	r.Dispatch("GET /v2/agents", func(r *route.Request) { // {{{
		agents, err := core.DB.GetAllAgents(nil)
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve agent information"))
			return
		}

		resp := struct {
			Agents   []*db.Agent         `json:"agents"`
			Problems map[string][]string `json:"problems"`
		}{
			Agents:   agents,
			Problems: make(map[string][]string),
		}

		for _, agent := range agents {
			id := agent.UUID.String()
			pp := make([]string, 0)

			if agent.Version == "" {
				pp = append(pp, Problems["legacy-shield-agent-version"])
			}
			if agent.Version == "dev" {
				pp = append(pp, Problems["dev-shield-agent-version"])
			}

			resp.Problems[id] = pp
		}
		r.OK(resp)
	})
	// }}}
	r.Dispatch("POST /v2/agents", func(r *route.Request) { // {{{
		var in struct {
			Name string `json:"name"`
			Port int    `json:"port"`
		}
		if !r.Payload(&in) {
			return
		}

		peer := regexp.MustCompile(`:\d+$`).ReplaceAllString(r.Req.RemoteAddr, "")
		if peer == "" {
			r.Fail(route.Oops(nil, "unable to determine remote peer address from '%s'", r.Req.RemoteAddr))
			return
		}

		if in.Name == "" {
			r.Fail(route.Bad(nil, "no `name' provided with pre-registration request"))
			return
		}
		if in.Port == 0 {
			r.Fail(route.Bad(nil, "no `port' provided with pre-registration request"))
			return
		}

		err := core.DB.PreRegisterAgent(peer, in.Name, in.Port)
		if err != nil {
			r.Fail(route.Oops(err, "failed to pre-register agent %s at %s:%i", in.Name, peer, in.Port))
			return
		}
		r.Success("pre-registered agent %s at %s:%i", in.Name, peer, in.Port)
	})
	// }}}

	r.Dispatch("GET /v2/tenants", func(r *route.Request) { // {{{
		tenants, err := core.DB.GetAllTenants()
		if err != nil {
			r.Fail(route.Oops(err, "failed to retrieve tenants information"))
			return
		}
		r.OK(tenants)
	})
	// }}}
	r.Dispatch("POST /v2/tenants", func(r *route.Request) { // {{{
		var in struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		}
		if !r.Payload(&in) {
			return
		}

		/* FIXME: need a better way of doing Missing Parameters */
		e := MissingParameters()
		e.Check("name", in.Name)
		if e.IsValid() {
			r.Fail(route.Bad(e, "%s", e))
			return
		}

		if strings.ToLower(in.Name) == "system" {
			r.Fail(route.Bad(nil, "tenant name 'system' is reserved"))
			return
		}

		t, err := core.DB.CreateTenant(in.UUID, in.Name)
		if err != nil {
			r.Fail(route.Oops(err, "failed to create new tenant '%s'", in.Name))
			return
		}
		r.OK(t)
	})
	// }}}
	r.Dispatch("PUT /v2/tenant/:uuid", func(r *route.Request) { // {{{
		var in struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		}
		if !r.Payload(&in) {
			return
		}

		/* FIXME: need a better way of doing Missing Parameters */
		e := MissingParameters()
		e.Check("uuid", in.UUID)
		e.Check("name", in.Name)
		if e.IsValid() {
			r.Fail(route.Bad(e, "%s", e))
			return
		}

		t, err := core.DB.UpdateTenant(in.UUID, in.Name)
		if err != nil {
			r.Fail(route.Oops(err, "failed to update tenant '%s'", in.Name))
			return
		}
		r.OK(t)
	})
	// }}}
	r.Dispatch("PATCH /v2/tenant/:uuid", func(r *route.Request) { // {{{
		/* FIXME */
		r.Fail(route.Errorf(501, nil, "%s: not implemented", r))
	})
	// }}}
	r.Dispatch("PATCH /v2/tenant/:uuid", func(r *route.Request) { // {{{
		/* FIXME */
		r.Fail(route.Errorf(501, nil, "%s: not implemented", r))
	})
	// }}}

	return r
}
package adminapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func (h *Handler) createVenue(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createVenueRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_VENUE",
		"",
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.venue.CreateVenue(
				ctx,
				userID,
				venuesvc.CreateVenueInput{
					Name:        request.Name,
					AddressText: request.AddressText,
					Metadata:    request.Metadata,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Venue,
						id,
					),
				},
			)
		},
	)
}

func (h *Handler) listVenues(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				id,
				name,
				address_text,
				created_at,
				updated_at
			FROM venues
			ORDER BY name, id
		`,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			id          uuid.UUID
			name        string
			addressText *string
			createdAt   any
			updatedAt   any
		)

		if err := rows.Scan(
			&id,
			&name,
			&addressText,
			&createdAt,
			&updatedAt,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		items = append(
			items,
			map[string]any{
				"id": publicid.Encode(
					publicid.Venue,
					id,
				),
				"name":         name,
				"address_text": addressText,
				"created_at":   createdAt,
				"updated_at":   updatedAt,
			},
		)
	}

	if err := rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"venues": items,
		},
	)
}

func (h *Handler) getVenue(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	id, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result, err := h.venueResponse(
		r.Context(),
		id,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		result,
	)
}

func (h *Handler) createLayoutVersion(
	w http.ResponseWriter,
	r *http.Request,
) {
	venueID, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_LAYOUT_VERSION",
		venueID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, version, err :=
				h.venue.CreateLayoutVersion(
					ctx,
					userID,
					venueID,
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						id,
					),
					"venue_id": publicid.Encode(
						publicid.Venue,
						venueID,
					),
					"version_number": version,
					"state":          "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) listLayoutVersions(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	venueID, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				id,
				version_number,
				state,
				published_at,
				retired_at,
				created_at
			FROM venue_layout_versions
			WHERE venue_id = $1
			ORDER BY version_number
		`,
		venueID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			id            uuid.UUID
			versionNumber int
			state         string
			publishedAt   any
			retiredAt     any
			createdAt     any
		)

		if err := rows.Scan(
			&id,
			&versionNumber,
			&state,
			&publishedAt,
			&retiredAt,
			&createdAt,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		items = append(
			items,
			map[string]any{
				"id": publicid.Encode(
					publicid.VenueLayout,
					id,
				),
				"version_number": versionNumber,
				"state":          state,
				"published_at":   publishedAt,
				"retired_at":     retiredAt,
				"created_at":     createdAt,
			},
		)
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"layout_versions": items,
		},
	)
}

func (h *Handler) getLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var (
		venueID     uuid.UUID
		version     int
		state       string
		geometry    []byte
		sections    []byte
		rows        []byte
		tables      []byte
		seats       []byte
		gaZones     []byte
		contentHash []byte
		publishedAt any
		retiredAt   any
		createdAt   any
	)

	err = h.db.QueryRow(
		r.Context(),
		`
			SELECT
				venue_id,
				version_number,
				state,
				geometry_json,
				COALESCE((SELECT jsonb_agg(jsonb_build_object('object_key',object_key,'name',name,'kind',section_kind,'sort_order',sort_order,'metadata',metadata) ORDER BY sort_order,object_key) FROM venue_layout_sections WHERE layout_version_id=$1),'[]'::jsonb),
				COALESCE((SELECT jsonb_agg(jsonb_build_object('object_key',r.object_key,'section_key',s.object_key,'label',r.label,'sort_order',r.sort_order,'metadata',r.metadata) ORDER BY r.sort_order,r.object_key) FROM venue_layout_rows r JOIN venue_layout_sections s ON s.id=r.section_id WHERE r.layout_version_id=$1),'[]'::jsonb),
				COALESCE((SELECT jsonb_agg(jsonb_build_object('object_key',t.object_key,'section_key',s.object_key,'label',t.label,'metadata',t.metadata) ORDER BY t.object_key) FROM venue_layout_tables t JOIN venue_layout_sections s ON s.id=t.section_id WHERE t.layout_version_id=$1),'[]'::jsonb),
				COALESCE((SELECT jsonb_agg(jsonb_build_object('object_key',seat.object_key,'section_key',s.object_key,'row_key',COALESCE(r.object_key,''),'table_key',COALESCE(t.object_key,''),'seat_label',seat.seat_label,'sort_order',seat.sort_order,'metadata',seat.metadata) ORDER BY s.sort_order,COALESCE(r.sort_order,0),seat.sort_order,seat.object_key) FROM venue_layout_seats seat JOIN venue_layout_sections s ON s.id=seat.section_id LEFT JOIN venue_layout_rows r ON r.id=seat.row_id LEFT JOIN venue_layout_tables t ON t.id=seat.table_id WHERE seat.layout_version_id=$1),'[]'::jsonb),
				COALESCE((SELECT jsonb_agg(jsonb_build_object('object_key',g.object_key,'section_key',s.object_key,'name',g.name,'default_capacity',g.default_capacity,'metadata',g.metadata) ORDER BY s.sort_order,g.object_key) FROM venue_layout_ga_zones g JOIN venue_layout_sections s ON s.id=g.section_id WHERE g.layout_version_id=$1),'[]'::jsonb),
				content_hash,
				published_at,
				retired_at,
				created_at
			FROM venue_layout_versions
			WHERE id = $1
		`,
		layoutID,
	).Scan(
		&venueID,
		&version,
		&state,
		&geometry,
		&sections,
		&rows,
		&tables,
		&seats,
		&gaZones,
		&contentHash,
		&publishedAt,
		&retiredAt,
		&createdAt,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"id": publicid.Encode(
				publicid.VenueLayout,
				layoutID,
			),
			"venue_id": publicid.Encode(
				publicid.Venue,
				venueID,
			),
			"version_number": version,
			"state":          state,
			"geometry":       rawJSON(geometry),
			"sections":       rawJSON(sections),
			"rows":           rawJSON(rows),
			"tables":         rawJSON(tables),
			"seats":          rawJSON(seats),
			"ga_zones":       rawJSON(gaZones),
			"content_hash":   contentHash,
			"published_at":   publishedAt,
			"retired_at":     retiredAt,
			"created_at":     createdAt,
		},
	)
}

func (h *Handler) replaceLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request replaceLayoutRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	sections := make(
		[]venuesvc.SectionInput,
		0,
		len(request.Sections),
	)
	for _, item := range request.Sections {
		sections = append(
			sections,
			venuesvc.SectionInput{
				ObjectKey: item.ObjectKey,
				Name:      item.Name,
				Kind:      item.Kind,
				SortOrder: item.SortOrder,
				Metadata:  item.Metadata,
			},
		)
	}

	rows := make(
		[]venuesvc.RowInput,
		0,
		len(request.Rows),
	)
	for _, item := range request.Rows {
		rows = append(
			rows,
			venuesvc.RowInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				Label:      item.Label,
				SortOrder:  item.SortOrder,
				Metadata:   item.Metadata,
			},
		)
	}

	tables := make(
		[]venuesvc.TableInput,
		0,
		len(request.Tables),
	)
	for _, item := range request.Tables {
		tables = append(
			tables,
			venuesvc.TableInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				Label:      item.Label,
				Metadata:   item.Metadata,
			},
		)
	}

	seats := make(
		[]venuesvc.SeatInput,
		0,
		len(request.Seats),
	)
	for _, item := range request.Seats {
		seats = append(
			seats,
			venuesvc.SeatInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				RowKey:     item.RowKey,
				TableKey:   item.TableKey,
				SeatLabel:  item.SeatLabel,
				SortOrder:  item.SortOrder,
				Metadata:   item.Metadata,
			},
		)
	}

	gaZones := make(
		[]venuesvc.GAZoneInput,
		0,
		len(request.GAZones),
	)
	for _, item := range request.GAZones {
		gaZones = append(
			gaZones,
			venuesvc.GAZoneInput{
				ObjectKey:       item.ObjectKey,
				SectionKey:      item.SectionKey,
				Name:            item.Name,
				DefaultCapacity: item.DefaultCapacity,
				Metadata:        item.Metadata,
			},
		)
	}

	h.runMutation(
		w,
		r,
		"ADMIN_REPLACE_LAYOUT_DRAFT",
		layoutID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.venue.ReplaceDraftLayout(
				ctx,
				userID,
				layoutID,
				venuesvc.ReplaceLayoutInput{
					Geometry: request.Geometry,
					Sections: sections,
					Rows:     rows,
					Tables:   tables,
					Seats:    seats,
					GAZones:  gaZones,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"state": "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) publishLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_PUBLISH_LAYOUT",
		layoutID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.venue.PublishLayout(
				ctx,
				userID,
				layoutID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"state": "PUBLISHED",
				},
			)
		},
	)
}

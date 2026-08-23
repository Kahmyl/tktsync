package adminapi

type gaRestrictionTargetRequest struct {
	InventoryID string `json:"inventory_id"`
	Quantity    int    `json:"quantity"`
}

type createBlockRequest struct {
	Purpose              string                       `json:"purpose"`
	Reason               string                       `json:"reason,omitempty"`
	ReservedInventoryIDs []string                     `json:"reserved_inventory_ids,omitempty"`
	GATargets            []gaRestrictionTargetRequest `json:"ga_targets,omitempty"`
}

type releaseDestinationRequest struct {
	Kind         string `json:"kind"`
	AllocationID string `json:"allocation_id,omitempty"`
}

type createAllocationRequest struct {
	Mode                 string                       `json:"mode"`
	PartnerID            string                       `json:"partner_id,omitempty"`
	Purpose              string                       `json:"purpose"`
	Reason               string                       `json:"reason,omitempty"`
	ReleaseDestination   *releaseDestinationRequest   `json:"release_destination,omitempty"`
	ReservedInventoryIDs []string                     `json:"reserved_inventory_ids,omitempty"`
	GATargets            []gaRestrictionTargetRequest `json:"ga_targets,omitempty"`
}

type reclassifyAllocationRequest struct {
	Mode      string `json:"mode"`
	PartnerID string `json:"partner_id,omitempty"`
}

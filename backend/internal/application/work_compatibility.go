package application

import "magistral/backend/internal/dto"

func WorkMatchesBuilding(code dto.WorkTypeCode, technology dto.Technology, building dto.BuildingType) bool {
	switch code {
	case "INST_PON_MKD":
		return technology == "PON" && building == "MKD"
	case "INST_FTTB_MKD":
		return technology == "FTTB" && building == "MKD"
	case "INST_PRIVATE_VLS", "SURVEY_PRIVATE":
		return building == "PRIVATE"
	default:
		return true
	}
}

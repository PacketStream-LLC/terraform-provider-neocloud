package client

import "testing"

// 생성물이 프로바이더가 기대는 대표 타입을 실제로 담고 있는지 — 스펙 갱신으로
// 이름이 사라지면 여기서 컴파일이 깨져 드러난다.
func TestGeneratedTypesExist(t *testing.T) {
	_ = VirtualNetworkDto{}
	_ = VirtualMachineCreateRequest{}
	_ = BlockStorageUpdateRequest{}
	_ = ApiProblemDetail{}
	_ = MutationResultDto{}
	var c *ClientWithResponses
	_ = c
	t.Log("generated client types compile")
}

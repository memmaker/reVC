#pragma once
// Modernization layer (goplay Phase 2): analog triggers, first person, over-the-shoulder aim,
// weapon/radio wheels, drive-by free aim, GPS route, cover. Minimal versions, see CHECKLIST.md.
class CPlayerPed;
class CPad;
class CVehicle;
class CVector;

namespace Modern {
	extern float TriggerL, TriggerR;	// 0..1 analog triggers from the pad
	extern bool bFirstPerson;			// ped camera sits on the head bone
	extern bool bWheelOpen;				// weapon or radio wheel showing
	extern bool bInCover;

	void ProcessPlayer(CPlayerPed *ped, CPad *pad);	// wheels + cover, from CPlayerPed::ProcessControl
	void DrawHud();									// wheels, end of CHud::Draw
	void DrawGPS();									// route on the radar, from CRadar::DrawBlips
	bool DriveByFree(CVehicle *veh);				// camera-aimed drive-by; true when it handled firing
	void CycleFirstPerson();						// called when the ped zoom wraps around
}

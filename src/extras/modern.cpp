#include "common.h"
#include "modern.h"
#include "Camera.h"
#include "Pad.h"
#include "Timer.h"
#include "PlayerPed.h"
#include "Vehicle.h"
#include "Weapon.h"
#include "WeaponInfo.h"
#include "ModelInfo.h"
#include "TxdStore.h"
#include "Sprite2d.h"
#include "Font.h"
#include "Text.h"
#include "Hud.h"
#include "Radar.h"
#include "PathFind.h"
#include "World.h"
#include "DMAudio.h"
#include "AnimBlendAssociation.h"
#include "AnimManager.h"
#include "RpAnimBlend.h"
#include "Automobile.h"
#include "General.h"
#include "Frontend.h"

namespace Modern {

float TriggerL, TriggerR;
bool bFirstPerson;
bool bWheelOpen;
bool bInCover;

static int wheelSlot = -1;		// highlighted weapon slot
static int radioSel = -1;		// highlighted station
static uint32 holdStart;		// when the wheel key went down
static float wheelX, wheelY;	// accumulated mouse delta
static CPlayerPed *wheelPed;

void CycleFirstPerson() { bFirstPerson = !bFirstPerson; }

// ---- wheels -----------------------------------------------------------------

static int
PickSector(int n)
{
	if(wheelX*wheelX + wheelY*wheelY < 400.0f) return -1;
	float a = CGeneral::GetATanOfXY(wheelX, -wheelY);	// 0 = right, ccw
	int s = (int)((a + PI/n + TWOPI) / TWOPI * n) % n;
	return s;
}

static void
Wheel(CPad *pad, bool key, int n, int &sel, void (*apply)(CPlayerPed*, int), CPlayerPed *ped)
{
	if(key){
		if(holdStart == 0){ holdStart = CTimer::GetTimeInMilliseconds(); wheelX = wheelY = 0.0f; }
		if(!bWheelOpen && CTimer::GetTimeInMilliseconds() - holdStart > 200){
			bWheelOpen = true;
			CTimer::SetTimeScale(0.1f);
		}
		if(bWheelOpen){
			wheelX += pad->GetMouseX();
			wheelY += pad->GetMouseY();
			int s = PickSector(n);
			if(s >= 0) sel = s;
		}
	}else if(holdStart){
		if(bWheelOpen){
			CTimer::SetTimeScale(1.0f);
			if(sel >= 0) apply(ped, sel);
		}
		holdStart = 0; bWheelOpen = false; sel = -1;
	}
}

static void
ApplyWeapon(CPlayerPed *ped, int slot)
{
	if(ped->HasWeaponSlot(slot) || slot == WEAPONSLOT_UNARMED)
		ped->m_nSelectedWepSlot = slot;	// ProcessWeaponSwitch does the actual change
}

static void
ApplyRadio(CPlayerPed *ped, int station)
{
	if(ped->m_pMyVehicle) ped->m_pMyVehicle->m_nRadioStation = station;
	DMAudio.SetRadioInCar(station);
}

void
ProcessPlayer(CPlayerPed *ped, CPad *pad)
{
	wheelPed = ped;
	// save anywhere: F5 opens the save menu like a save point would
	if(pad->GetFJustDown(4) && !FrontEndMenuManager.m_bMenuActive)
		FrontEndMenuManager.m_bActivateSaveMenu = true;
	// first person: hide the player's own model, give it back when we leave
	static bool hid;
	if(bFirstPerson && !ped->bInVehicle){ ped->bIsVisible = false; hid = true; }
	else if(hid){ ped->bIsVisible = true; hid = false; }
	if(ped->bInVehicle && ped->m_pMyVehicle){
		// ponytail: a tap still falls through to the stock "next station" handler
		Wheel(pad, pad->GetChar('R'), NUM_RADIOS + 1, radioSel, ApplyRadio, ped);
		bInCover = false;
		return;
	}
	Wheel(pad, pad->GetTab(), TOTAL_WEAPON_SLOTS, wheelSlot, ApplyWeapon, ped);

	// ---- cover: duck next to a wall snaps you to it, aiming pops you out ----
	if(pad->DuckJustDown() && !bInCover && !ped->bIsDucking){
		CVector fwd = ped->GetForward();
		CColPoint col; CEntity *ent;
		if(CWorld::ProcessLineOfSight(ped->GetPosition(), ped->GetPosition() + fwd*1.2f, col, ent, true, false, false, true, false, true)){
			bInCover = true;
			CVector n = col.normal; n.z = 0.0f; n.Normalise();
			ped->SetPosition(col.point + n*0.35f);
			ped->m_fRotationCur = ped->m_fRotationDest = CGeneral::GetATanOfXY(n.x, n.y) + HALFPI;	// back to the wall
		}
	}
	if(bInCover){
		if(pad->GetPedWalkLeftRight() || pad->GetPedWalkUpDown() || pad->JumpJustDown() || ped->bInVehicle)
			bInCover = false;
		else if(pad->GetTarget()){
			if(ped->bIsDucking) ped->ClearDuck(true);	// pop out to aim
		}else if(!ped->bIsDucking)
			ped->SetDuck(60000, true);
	}
}

static void
DrawIcon(int32 weaponType, const CRect &r, const CRGBA &c)
{
	CWeaponInfo *info = CWeaponInfo::GetWeaponInfo((eWeaponType)weaponType);
	if(info->m_nModelId <= 0){
		CHud::Sprites[weaponType].Draw(r, c, 0.015f, 0.015f, 1.0f, 0.0f, 0.015f, 1.0f, 1.0f, 1.0f);
		return;
	}
	CBaseModelInfo *mi = CModelInfo::GetModelInfo(info->m_nModelId);
	RwTexDictionary *txd = CTxdStore::GetSlot(mi->GetTxdSlot())->texDict;
	RwTexture *tex = txd ? RwTexDictionaryFindNamedTexture(txd, mi->GetModelName()) : nil;
	if(!tex) return;
	static CSprite2d sprite;
	sprite.m_pTexture = tex;
	sprite.Draw(r, c, 0.015f, 0.015f, 1.0f, 0.0f, 0.015f, 1.0f, 1.0f, 1.0f);
	sprite.m_pTexture = nil;
}

void
DrawHud()
{
	if(!bWheelOpen || !wheelPed) return;
	RwRenderStateSet(rwRENDERSTATETEXTUREFILTER, (void*)rwFILTERLINEAR);
	float cx = SCREEN_WIDTH/2, cy = SCREEN_HEIGHT/2, rad = SCREEN_SCALE_Y(150.0f);
	bool radio = wheelPed->bInVehicle;
	int n = radio ? NUM_RADIOS + 1 : TOTAL_WEAPON_SLOTS;
	int sel = radio ? radioSel : wheelSlot;
	CFont::SetPropOn(); CFont::SetBackgroundOff(); CFont::SetCentreOn(); CFont::SetJustifyOff();
	CFont::SetFontStyle(FONT_STANDARD); CFont::SetDropShadowPosition(1); CFont::SetDropColor(CRGBA(0,0,0,255));
	CSprite2d::DrawRect(CRect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT), CRGBA(0, 0, 0, 110));
	for(int i = 0; i < n; i++){
		float a = i * TWOPI / n;
		float x = cx + Cos(a)*rad, y = cy - Sin(a)*rad;
		CRGBA col = i == sel ? CRGBA(255, 220, 80, 255) : CRGBA(255, 255, 255, 170);
		if(radio){
			static const char *keys[] = { "FEA_FM0","FEA_FM1","FEA_FM2","FEA_FM3","FEA_FM4","FEA_FM5","FEA_FM6","FEA_FM7","FEA_FM8","FEA_MP3","FEM_OFF" };
			CFont::SetScale(SCREEN_SCALE_X(0.5f), SCREEN_SCALE_Y(0.8f));
			CFont::SetColor(col);
			CFont::PrintString(x, y - SCREEN_SCALE_Y(10.0f), TheText.Get(keys[i]));
		}else{
			CWeapon &w = wheelPed->m_weapons[i];
			if(i != WEAPONSLOT_UNARMED && w.m_eWeaponType == WEAPONTYPE_UNARMED) col.a = 60;
			float s = SCREEN_SCALE_Y(32.0f);
			DrawIcon(w.m_eWeaponType, CRect(x - s, y - s, x + s, y + s), col);
			if(w.m_nAmmoTotal > 0){
				char buf[16]; sprintf(buf, "%d", w.m_nAmmoTotal);
				wchar wbuf[16]; AsciiToUnicode(buf, wbuf);
				CFont::SetScale(SCREEN_SCALE_X(0.4f), SCREEN_SCALE_Y(0.7f));
				CFont::SetColor(CRGBA(255, 255, 255, 255));
				CFont::PrintString(x, y + s, wbuf);
			}
		}
	}
}

// ---- GPS ----------------------------------------------------------------------

static CVector gpsTarget;
static bool gpsHasTarget;

static bool
FindGPSTarget(CVector &out)
{
	if(CRadar::TargetMarkerId != -1){ out = CRadar::TargetMarkerPos; return true; }
	for(int i = 0; i < NUMRADARBLIPS; i++){
		sRadarTrace &b = CRadar::ms_RadarTrace[i];
		if(b.m_bInUse && b.m_eBlipType == BLIP_COORD && b.m_eRadarSprite == RADAR_SPRITE_NONE){ out = b.m_vecPos; return true; }
	}
	return false;
}

static void
RadarLine(CVector2D a, CVector2D b, const CRGBA &col)
{
	// clip to the radar disc (unit circle) ponytail: drop segments crossing the edge instead of clipping them
	if(a.MagnitudeSqr() > 1.0f && b.MagnitudeSqr() > 1.0f) return;
	if(a.MagnitudeSqr() > 1.0f) a *= 0.999f / a.Magnitude();
	if(b.MagnitudeSqr() > 1.0f) b *= 0.999f / b.Magnitude();
	CVector2D sa, sb;
	CRadar::TransformRadarPointToScreenSpace(sa, a);
	CRadar::TransformRadarPointToScreenSpace(sb, b);
	CVector2D d = sb - sa;
	float len = d.Magnitude();
	if(len < 0.5f) return;
	CVector2D n = CVector2D(-d.y, d.x) * (SCREEN_SCALE_X(1.5f) / len);
	CSprite2d::Draw2DPolygon(sa.x + n.x, sa.y + n.y, sb.x + n.x, sb.y + n.y, sa.x - n.x, sa.y - n.y, sb.x - n.x, sb.y - n.y, col);
}

void
DrawGPS()
{
	static CPathNode *nodes[64];
	static int16 numNodes;
	static uint32 lastSearch;
	static CVector lastTarget;
	CVector target;
	if(!FindGPSTarget(target)){ numNodes = 0; return; }
	CVector player = FindPlayerCoors();
	if(CTimer::GetTimeInMilliseconds() - lastSearch > 1000 || (target - lastTarget).MagnitudeSqr() > 1.0f){
		float dist;
		ThePaths.DoPathSearch(PATH_CAR, player, -1, target, nodes, &numNodes, 64, nil, &dist, 999999.0f, -1);
		lastSearch = CTimer::GetTimeInMilliseconds(); lastTarget = target;
	}
	CRGBA col(140, 60, 255, 220);
	CVector2D prev; CRadar::TransformRealWorldPointToRadarSpace(prev, CVector2D(player.x, player.y));
	for(int i = 0; i < numNodes; i++){
		CVector p = nodes[i]->GetPosition();
		CVector2D cur; CRadar::TransformRealWorldPointToRadarSpace(cur, CVector2D(p.x, p.y));
		RadarLine(prev, cur, col); prev = cur;
	}
	CVector2D end; CRadar::TransformRealWorldPointToRadarSpace(end, CVector2D(target.x, target.y));
	RadarLine(prev, end, col);
}

// ---- drive-by free aim ----------------------------------------------------------

bool
DriveByFree(CVehicle *veh)
{
	if(!CCamera::bFreeCam || !CCamera::m_bUseMouse3rdPerson || !veh->IsCar()) return false;
	CPed *driver = veh->pDriver;
	CWeapon *weapon = driver->GetWeapon();
	CVector dir = TheCamera.Cams[TheCamera.ActiveCam].Front;
	bool left = DotProduct(dir, veh->GetRight()) < 0.0f;
	CAutomobile *car = (CAutomobile*)veh;
	AnimationId want = left ? (veh->bLowVehicle ? ANIM_STD_CAR_DRIVEBY_LEFT_LO : ANIM_STD_CAR_DRIVEBY_LEFT)
	                        : (veh->bLowVehicle ? ANIM_STD_CAR_DRIVEBY_RIGHT_LO : ANIM_STD_CAR_DRIVEBY_RIGHT);
	AnimationId other = left ? (veh->bLowVehicle ? ANIM_STD_CAR_DRIVEBY_RIGHT_LO : ANIM_STD_CAR_DRIVEBY_RIGHT)
	                         : (veh->bLowVehicle ? ANIM_STD_CAR_DRIVEBY_LEFT_LO : ANIM_STD_CAR_DRIVEBY_LEFT);
	if(!CPad::GetPad(0)->GetCarGunFired()) return false;	// not shooting: stock code tidies anims
	CAnimBlendAssociation *anim = RpAnimBlendClumpGetAssociation(driver->GetClump(), other);
	if(anim) anim->blendDelta = -1000.0f;
	anim = RpAnimBlendClumpGetAssociation(driver->GetClump(), want);
	if(anim == nil || anim->blendDelta < 0.0f)
		anim = CAnimManager::AddAnimation(driver->GetClump(), ASSOCGRP_STD, want);
	if((!anim || !anim->IsRunning()) && CTimer::GetTimeInMilliseconds() > weapon->m_nTimer){
		weapon->FireFromCarFree(veh, dir);
		weapon->m_nTimer = CTimer::GetTimeInMilliseconds() + 70;
	}
	(void)car;
	return true;
}

}

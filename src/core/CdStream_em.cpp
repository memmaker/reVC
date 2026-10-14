// Synchronous CdStream for Emscripten: no threads in the browser build, the
// image lives in MEMFS, so a request is served the moment it is made and the
// status API reports it as finished. Same interface as CdStream_posix.cpp.
#ifdef __EMSCRIPTEN__
#include <errno.h>
#include "common.h"
#include "crossplatform.h"
#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include "CdStream.h"
#include "rwcore.h"
#include "MemoryMgr.h"

#ifdef FLUSHABLE_STREAMING
bool flushStream[MAX_CDCHANNELS];
#endif

struct CdReadInfo
{
	int32 nStatus; // result of the last read, STREAM_NONE when fine
};

static CdReadInfo *gpReadInfo;
static int32 gNumChannels;
static int32 gNumImages;
static int32 gImgFiles[MAX_CDIMAGES]; // -1: error 0: unused otherwise: fd+1
static int32 lastPosnRead;
static char gCdImageNames[MAX_CDIMAGES+1][64];

void CdStreamInitThread(void) {}
void AddToQueue(Queue *, int32) {}
int32 GetFirstInQueue(Queue *) { return -1; }
void RemoveFirstInQueue(Queue *) {}

void
CdStreamInit(int32 numChannels)
{
	gNumImages = 0;
	gNumChannels = numChannels;
	gpReadInfo = (CdReadInfo *)calloc(numChannels, sizeof(CdReadInfo));
	ASSERT(gpReadInfo != nil);
}

uint32
GetGTA3ImgSize(void)
{
	struct stat st;
	ASSERT(gImgFiles[0] > 0);
	if(fstat(gImgFiles[0] - 1, &st) == -1)
		return 0;
	return (uint32)st.st_size;
}

void
CdStreamShutdown(void)
{
	free(gpReadInfo);
	gpReadInfo = nil;
}

int32
CdStreamRead(int32 channel, void *buffer, uint32 offset, uint32 size)
{
	ASSERT(channel < gNumChannels);
	ASSERT(buffer != nil);
	lastPosnRead = size + offset;
	int32 fd = gImgFiles[_GET_INDEX(offset)];
	ASSERT(fd > 0);
	size_t want = (size_t)size * CDSTREAM_SECTOR_SIZE;
	ssize_t got = pread(fd - 1, buffer, want, (off_t)_GET_OFFSET(offset) * CDSTREAM_SECTOR_SIZE);
	// a read past the end of the image is still a full sector count for the caller
	gpReadInfo[channel].nStatus = got < 0 ? STREAM_ERROR : STREAM_NONE;
	return STREAM_SUCCESS;
}

int32
CdStreamGetStatus(int32 channel)
{
	ASSERT(channel < gNumChannels);
	int32 status = gpReadInfo[channel].nStatus;
	gpReadInfo[channel].nStatus = STREAM_NONE;
	return status;
}

int32
CdStreamGetLastPosn(void)
{
	return lastPosnRead;
}

int32
CdStreamSync(int32 channel)
{
	ASSERT(channel < gNumChannels);
#ifdef FLUSHABLE_STREAMING
	flushStream[channel] = false;
#endif
	return gpReadInfo[channel].nStatus;
}

bool
CdStreamAddImage(char const *path)
{
	ASSERT(path != nil);
	ASSERT(gNumImages < MAX_CDIMAGES);
	int fd = open(path, O_RDONLY);
	if(fd == -1){
		char *real = casepath(path, false);
		if(real){
			fd = open(real, O_RDONLY);
			free(real);
		}
	}
	if(fd == -1){
		fprintf(stderr, "CdStreamAddImage: cannot open %s: %s\n", path, strerror(errno));
		return false;
	}
	gImgFiles[gNumImages] = fd + 1;
	strcpy(gCdImageNames[gNumImages], path);
	gNumImages++;
	return true;
}

char *
CdStreamGetImageName(int32 cd)
{
	ASSERT(cd < MAX_CDIMAGES);
	if(gImgFiles[cd] > 0)
		return gCdImageNames[cd];
	return nil;
}

void
CdStreamRemoveImages(void)
{
	for(int32 i = 0; i < gNumImages; i++){
		close(gImgFiles[i] - 1);
		gImgFiles[i] = 0;
	}
	gNumImages = 0;
}

int32
CdStreamGetNumImages(void)
{
	return gNumImages;
}
#endif

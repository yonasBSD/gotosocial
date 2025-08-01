/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2017 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/minio/minio-go/v7/pkg/s3utils"
	"github.com/minio/minio-go/v7/pkg/tags"
	"golang.org/x/net/http/httpguts"
)

// ReplicationStatus represents replication status of object
type ReplicationStatus string

const (
	// ReplicationStatusPending indicates replication is pending
	ReplicationStatusPending ReplicationStatus = "PENDING"
	// ReplicationStatusComplete indicates replication completed ok
	ReplicationStatusComplete ReplicationStatus = "COMPLETED"
	// ReplicationStatusFailed indicates replication failed
	ReplicationStatusFailed ReplicationStatus = "FAILED"
	// ReplicationStatusReplica indicates object is a replica of a source
	ReplicationStatusReplica ReplicationStatus = "REPLICA"
	// ReplicationStatusReplicaEdge indicates object is a replica of a edge source
	ReplicationStatusReplicaEdge ReplicationStatus = "REPLICA-EDGE"
)

// Empty returns true if no replication status set.
func (r ReplicationStatus) Empty() bool {
	return r == ""
}

// AdvancedPutOptions for internal use - to be utilized by replication, ILM transition
// implementation on MinIO server
type AdvancedPutOptions struct {
	SourceVersionID          string
	SourceETag               string
	ReplicationStatus        ReplicationStatus
	SourceMTime              time.Time
	ReplicationRequest       bool
	RetentionTimestamp       time.Time
	TaggingTimestamp         time.Time
	LegalholdTimestamp       time.Time
	ReplicationValidityCheck bool
}

// PutObjectOptions represents options specified by user for PutObject call
type PutObjectOptions struct {
	UserMetadata            map[string]string
	UserTags                map[string]string
	Progress                io.Reader
	ContentType             string
	ContentEncoding         string
	ContentDisposition      string
	ContentLanguage         string
	CacheControl            string
	Expires                 time.Time
	Mode                    RetentionMode
	RetainUntilDate         time.Time
	ServerSideEncryption    encrypt.ServerSide
	NumThreads              uint
	StorageClass            string
	WebsiteRedirectLocation string
	PartSize                uint64
	LegalHold               LegalHoldStatus
	SendContentMd5          bool
	DisableContentSha256    bool
	DisableMultipart        bool

	// AutoChecksum is the type of checksum that will be added if no other checksum is added,
	// like MD5 or SHA256 streaming checksum, and it is feasible for the upload type.
	// If none is specified CRC32C is used, since it is generally the fastest.
	AutoChecksum ChecksumType

	// Checksum will force a checksum of the specific type.
	// This requires that the client was created with "TrailingHeaders:true" option,
	// and that the destination server supports it.
	// Unavailable with V2 signatures & Google endpoints.
	// This will disable content MD5 checksums if set.
	Checksum ChecksumType

	// ConcurrentStreamParts will create NumThreads buffers of PartSize bytes,
	// fill them serially and upload them in parallel.
	// This can be used for faster uploads on non-seekable or slow-to-seek input.
	ConcurrentStreamParts bool
	Internal              AdvancedPutOptions

	customHeaders http.Header
}

// SetMatchETag if etag matches while PUT MinIO returns an error
// this is a MinIO specific extension to support optimistic locking
// semantics.
func (opts *PutObjectOptions) SetMatchETag(etag string) {
	if opts.customHeaders == nil {
		opts.customHeaders = http.Header{}
	}
	if etag == "*" {
		opts.customHeaders.Set("If-Match", "*")
	} else {
		opts.customHeaders.Set("If-Match", "\""+etag+"\"")
	}
}

// SetMatchETagExcept if etag does not match while PUT MinIO returns an
// error this is a MinIO specific extension to support optimistic locking
// semantics.
func (opts *PutObjectOptions) SetMatchETagExcept(etag string) {
	if opts.customHeaders == nil {
		opts.customHeaders = http.Header{}
	}
	if etag == "*" {
		opts.customHeaders.Set("If-None-Match", "*")
	} else {
		opts.customHeaders.Set("If-None-Match", "\""+etag+"\"")
	}
}

// getNumThreads - gets the number of threads to be used in the multipart
// put object operation
func (opts PutObjectOptions) getNumThreads() (numThreads int) {
	if opts.NumThreads > 0 {
		numThreads = int(opts.NumThreads)
	} else {
		numThreads = totalWorkers
	}
	return
}

// Header - constructs the headers from metadata entered by user in
// PutObjectOptions struct
func (opts PutObjectOptions) Header() (header http.Header) {
	header = make(http.Header)

	contentType := opts.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)

	if opts.ContentEncoding != "" {
		header.Set("Content-Encoding", opts.ContentEncoding)
	}
	if opts.ContentDisposition != "" {
		header.Set("Content-Disposition", opts.ContentDisposition)
	}
	if opts.ContentLanguage != "" {
		header.Set("Content-Language", opts.ContentLanguage)
	}
	if opts.CacheControl != "" {
		header.Set("Cache-Control", opts.CacheControl)
	}

	if !opts.Expires.IsZero() {
		header.Set("Expires", opts.Expires.UTC().Format(http.TimeFormat))
	}

	if opts.Mode != "" {
		header.Set(amzLockMode, opts.Mode.String())
	}

	if !opts.RetainUntilDate.IsZero() {
		header.Set("X-Amz-Object-Lock-Retain-Until-Date", opts.RetainUntilDate.Format(time.RFC3339))
	}

	if opts.LegalHold != "" {
		header.Set(amzLegalHoldHeader, opts.LegalHold.String())
	}

	if opts.ServerSideEncryption != nil {
		opts.ServerSideEncryption.Marshal(header)
	}

	if opts.StorageClass != "" {
		header.Set(amzStorageClass, opts.StorageClass)
	}

	if opts.WebsiteRedirectLocation != "" {
		header.Set(amzWebsiteRedirectLocation, opts.WebsiteRedirectLocation)
	}

	if !opts.Internal.ReplicationStatus.Empty() {
		header.Set(amzBucketReplicationStatus, string(opts.Internal.ReplicationStatus))
	}
	if !opts.Internal.SourceMTime.IsZero() {
		header.Set(minIOBucketSourceMTime, opts.Internal.SourceMTime.Format(time.RFC3339Nano))
	}
	if opts.Internal.SourceETag != "" {
		header.Set(minIOBucketSourceETag, opts.Internal.SourceETag)
	}
	if opts.Internal.ReplicationRequest {
		header.Set(minIOBucketReplicationRequest, "true")
	}
	if opts.Internal.ReplicationValidityCheck {
		header.Set(minIOBucketReplicationCheck, "true")
	}
	if !opts.Internal.LegalholdTimestamp.IsZero() {
		header.Set(minIOBucketReplicationObjectLegalHoldTimestamp, opts.Internal.LegalholdTimestamp.Format(time.RFC3339Nano))
	}
	if !opts.Internal.RetentionTimestamp.IsZero() {
		header.Set(minIOBucketReplicationObjectRetentionTimestamp, opts.Internal.RetentionTimestamp.Format(time.RFC3339Nano))
	}
	if !opts.Internal.TaggingTimestamp.IsZero() {
		header.Set(minIOBucketReplicationTaggingTimestamp, opts.Internal.TaggingTimestamp.Format(time.RFC3339Nano))
	}

	if len(opts.UserTags) != 0 {
		if tags, _ := tags.NewTags(opts.UserTags, true); tags != nil {
			header.Set(amzTaggingHeader, tags.String())
		}
	}

	for k, v := range opts.UserMetadata {
		if isAmzHeader(k) || isStandardHeader(k) || isStorageClassHeader(k) || isMinioHeader(k) {
			header.Set(k, v)
		} else {
			header.Set("x-amz-meta-"+k, v)
		}
	}

	// set any other additional custom headers.
	for k, v := range opts.customHeaders {
		header[k] = v
	}

	return
}

// validate() checks if the UserMetadata map has standard headers or and raises an error if so.
func (opts PutObjectOptions) validate(c *Client) (err error) {
	for k, v := range opts.UserMetadata {
		if !httpguts.ValidHeaderFieldName(k) || isStandardHeader(k) || isSSEHeader(k) || isStorageClassHeader(k) || isMinioHeader(k) {
			return errInvalidArgument(k + " unsupported user defined metadata name")
		}
		if !httpguts.ValidHeaderFieldValue(v) {
			return errInvalidArgument(v + " unsupported user defined metadata value")
		}
	}
	if opts.Mode != "" && !opts.Mode.IsValid() {
		return errInvalidArgument(opts.Mode.String() + " unsupported retention mode")
	}
	if opts.LegalHold != "" && !opts.LegalHold.IsValid() {
		return errInvalidArgument(opts.LegalHold.String() + " unsupported legal-hold status")
	}

	checkCrc := false
	for k := range opts.UserMetadata {
		if strings.HasPrefix(k, "x-amz-checksum-") {
			checkCrc = true
			break
		}
	}

	if opts.Checksum.IsSet() || checkCrc {
		switch {
		case !c.trailingHeaderSupport:
			return errInvalidArgument("Checksum requires Client with TrailingHeaders enabled")
		case c.overrideSignerType.IsV2():
			return errInvalidArgument("Checksum cannot be used with v2 signatures")
		case s3utils.IsGoogleEndpoint(*c.endpointURL):
			return errInvalidArgument("Checksum cannot be used with GCS endpoints")
		}
	}

	return nil
}

// completedParts is a collection of parts sortable by their part numbers.
// used for sorting the uploaded parts before completing the multipart request.
type completedParts []CompletePart

func (a completedParts) Len() int           { return len(a) }
func (a completedParts) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a completedParts) Less(i, j int) bool { return a[i].PartNumber < a[j].PartNumber }

// PutObject creates an object in a bucket.
//
// You must have WRITE permissions on a bucket to create an object.
//
//   - For size smaller than 16MiB PutObject automatically does a
//     single atomic PUT operation.
//
//   - For size larger than 16MiB PutObject automatically does a
//     multipart upload operation.
//
//   - For size input as -1 PutObject does a multipart Put operation
//     until input stream reaches EOF. Maximum object size that can
//     be uploaded through this operation will be 5TiB.
//
//     WARNING: Passing down '-1' will use memory and these cannot
//     be reused for best outcomes for PutObject(), pass the size always.
//
// NOTE: Upon errors during upload multipart operation is entirely aborted.
func (c *Client) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, size int64,
	opts PutObjectOptions,
) (info UploadInfo, err error) {
	if size < 0 && opts.DisableMultipart {
		return UploadInfo{}, errors.New("object size must be provided with disable multipart upload")
	}

	err = opts.validate(c)
	if err != nil {
		return UploadInfo{}, err
	}

	// Check for largest object size allowed.
	if size > int64(maxMultipartPutObjectSize) {
		return UploadInfo{}, errEntityTooLarge(size, maxMultipartPutObjectSize, bucketName, objectName)
	}

	if opts.Checksum.IsSet() {
		opts.AutoChecksum = opts.Checksum
		opts.SendContentMd5 = false
	}

	if c.trailingHeaderSupport {
		opts.AutoChecksum.SetDefault(ChecksumCRC32C)
		addAutoChecksumHeaders(&opts)
	}

	// NOTE: Streaming signature is not supported by GCS.
	if s3utils.IsGoogleEndpoint(*c.endpointURL) {
		return c.putObject(ctx, bucketName, objectName, reader, size, opts)
	}

	partSize := opts.PartSize
	if opts.PartSize == 0 {
		partSize = minPartSize
	}

	if c.overrideSignerType.IsV2() {
		if size >= 0 && size < int64(partSize) || opts.DisableMultipart {
			return c.putObject(ctx, bucketName, objectName, reader, size, opts)
		}
		return c.putObjectMultipart(ctx, bucketName, objectName, reader, size, opts)
	}

	if size < 0 {
		if opts.DisableMultipart {
			return UploadInfo{}, errors.New("no length provided and multipart disabled")
		}
		if opts.ConcurrentStreamParts && opts.NumThreads > 1 {
			return c.putObjectMultipartStreamParallel(ctx, bucketName, objectName, reader, opts)
		}
		return c.putObjectMultipartStreamNoLength(ctx, bucketName, objectName, reader, opts)
	}

	if size <= int64(partSize) || opts.DisableMultipart {
		return c.putObject(ctx, bucketName, objectName, reader, size, opts)
	}

	return c.putObjectMultipartStream(ctx, bucketName, objectName, reader, size, opts)
}

func (c *Client) putObjectMultipartStreamNoLength(ctx context.Context, bucketName, objectName string, reader io.Reader, opts PutObjectOptions) (info UploadInfo, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err = s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}

	// Total data read and written to server. should be equal to
	// 'size' at the end of the call.
	var totalUploadedSize int64

	// Complete multipart upload.
	var complMultipartUpload completeMultipartUpload

	// Calculate the optimal parts info for a given size.
	totalPartsCount, partSize, _, err := OptimalPartInfo(-1, opts.PartSize)
	if err != nil {
		return UploadInfo{}, err
	}

	// Initiate a new multipart upload.
	uploadID, err := c.newUploadID(ctx, bucketName, objectName, opts)
	if err != nil {
		return UploadInfo{}, err
	}

	defer func() {
		if err != nil {
			c.abortMultipartUpload(ctx, bucketName, objectName, uploadID)
		}
	}()

	// Part number always starts with '1'.
	partNumber := 1

	// Initialize parts uploaded map.
	partsInfo := make(map[int]ObjectPart)

	// Create a buffer.
	buf := make([]byte, partSize)

	// Create checksums
	// CRC32C is ~50% faster on AMD64 @ 30GB/s
	customHeader := make(http.Header)
	crc := opts.AutoChecksum.Hasher()

	for partNumber <= totalPartsCount {
		length, rerr := readFull(reader, buf)
		if rerr == io.EOF && partNumber > 1 {
			break
		}

		if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
			return UploadInfo{}, rerr
		}

		var md5Base64 string
		if opts.SendContentMd5 {
			// Calculate md5sum.
			hash := c.md5Hasher()
			hash.Write(buf[:length])
			md5Base64 = base64.StdEncoding.EncodeToString(hash.Sum(nil))
			hash.Close()
		}

		if opts.AutoChecksum.IsSet() {
			crc.Reset()
			crc.Write(buf[:length])
			cSum := crc.Sum(nil)
			customHeader.Set(opts.AutoChecksum.Key(), base64.StdEncoding.EncodeToString(cSum))
			customHeader.Set(amzChecksumAlgo, opts.AutoChecksum.String())
			if opts.AutoChecksum.FullObjectRequested() {
				customHeader.Set(amzChecksumMode, ChecksumFullObjectMode.String())
			}
		}

		// Update progress reader appropriately to the latest offset
		// as we read from the source.
		rd := newHook(bytes.NewReader(buf[:length]), opts.Progress)

		// Proceed to upload the part.
		p := uploadPartParams{bucketName: bucketName, objectName: objectName, uploadID: uploadID, reader: rd, partNumber: partNumber, md5Base64: md5Base64, size: int64(length), sse: opts.ServerSideEncryption, streamSha256: !opts.DisableContentSha256, customHeader: customHeader}
		objPart, uerr := c.uploadPart(ctx, p)
		if uerr != nil {
			return UploadInfo{}, uerr
		}

		// Save successfully uploaded part metadata.
		partsInfo[partNumber] = objPart

		// Save successfully uploaded size.
		totalUploadedSize += int64(length)

		// Increment part number.
		partNumber++

		// For unknown size, Read EOF we break away.
		// We do not have to upload till totalPartsCount.
		if rerr == io.EOF {
			break
		}
	}

	// Loop over total uploaded parts to save them in
	// Parts array before completing the multipart request.
	allParts := make([]ObjectPart, 0, len(partsInfo))
	for i := 1; i < partNumber; i++ {
		part, ok := partsInfo[i]
		if !ok {
			return UploadInfo{}, errInvalidArgument(fmt.Sprintf("Missing part number %d", i))
		}
		allParts = append(allParts, part)
		complMultipartUpload.Parts = append(complMultipartUpload.Parts, CompletePart{
			ETag:              part.ETag,
			PartNumber:        part.PartNumber,
			ChecksumCRC32:     part.ChecksumCRC32,
			ChecksumCRC32C:    part.ChecksumCRC32C,
			ChecksumSHA1:      part.ChecksumSHA1,
			ChecksumSHA256:    part.ChecksumSHA256,
			ChecksumCRC64NVME: part.ChecksumCRC64NVME,
		})
	}

	// Sort all completed parts.
	sort.Sort(completedParts(complMultipartUpload.Parts))

	opts = PutObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
		AutoChecksum:         opts.AutoChecksum,
	}
	applyAutoChecksum(&opts, allParts)

	uploadInfo, err := c.completeMultipartUpload(ctx, bucketName, objectName, uploadID, complMultipartUpload, opts)
	if err != nil {
		return UploadInfo{}, err
	}

	uploadInfo.Size = totalUploadedSize
	return uploadInfo, nil
}

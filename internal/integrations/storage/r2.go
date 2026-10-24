package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// R2Config son los datos de R2_* (.env).
type R2Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// Configured dice si están todos los datos.
func (c R2Config) Configured() bool {
	return c.Endpoint != "" && c.AccessKeyID != "" && c.SecretAccessKey != "" && c.Bucket != ""
}

// R2 guarda en Cloudflare R2 por su API compatible con S3.
type R2 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// operationTimeout acota cada llamada: R2 lento no debe colgar una petición.
const operationTimeout = 30 * time.Second

// deleteBatch es el máximo de llaves por DeleteObjects.
const deleteBatch = 1000

// NewR2 crea el cliente. La región de R2 es siempre "auto".
func NewR2(cfg R2Config) *R2 {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle: true,
	})
	return &R2{client: client, presign: s3.NewPresignClient(client), bucket: cfg.Bucket}
}

// Put sube un objeto.
func (r *R2) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(key), Body: body,
		ContentType: aws.String(contentType), ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("r2 put %s: %w", key, err)
	}
	return nil
}

// Get abre un objeto. El contexto de la petición acota la lectura: no se le
// pone un timeout propio porque el cuerpo se sigue leyendo después de volver.
func (r *R2) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("r2 get %s: %w", key, err)
	}
	return out.Body, nil
}

// Delete borra objetos (de a 1000 por llamada).
func (r *R2) Delete(ctx context.Context, keys ...string) error {
	for start := 0; start < len(keys); start += deleteBatch {
		end := min(start+deleteBatch, len(keys))
		objects := make([]types.ObjectIdentifier, 0, end-start)
		for _, key := range keys[start:end] {
			objects = append(objects, types.ObjectIdentifier{Key: aws.String(key)})
		}
		callCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		_, err := r.client.DeleteObjects(callCtx, &s3.DeleteObjectsInput{
			Bucket: aws.String(r.bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("r2 delete: %w", err)
		}
	}
	return nil
}

// DeletePrefix lista y borra todo lo que empieza con prefix.
func (r *R2) DeletePrefix(ctx context.Context, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket), Prefix: aws.String(prefix),
	})
	var keys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("r2 list %s: %w", prefix, err)
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return r.Delete(ctx, keys...)
}

// PresignGet firma una URL de descarga.
func (r *R2) PresignGet(ctx context.Context, key string, ttl time.Duration, opts GetOptions) (string, error) {
	disposition := "inline"
	if opts.Download {
		disposition = "attachment"
	}
	input := &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)}
	if opts.FileName != "" {
		input.ResponseContentDisposition = aws.String(mime.FormatMediaType(disposition, map[string]string{"filename": opts.FileName}))
	}
	if opts.ContentType != "" {
		input.ResponseContentType = aws.String(opts.ContentType)
	}
	signed, err := r.presign.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("r2 presign %s: %w", key, err)
	}
	return signed.URL, nil
}

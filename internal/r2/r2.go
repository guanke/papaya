package r2

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// Client manages R2 interactions.
type Client struct {
	svc        *s3.S3
	bucket     string
	publicURL  string
}

// New creates a new R2 client.
func New(accountID, accessKey, secretKey, bucket, publicURL string) (*Client, error) {
	if accountID == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("missing R2 credentials")
	}

	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("auto"), // R2 uses 'auto'
		S3ForcePathStyle: aws.Bool(true),
	}

	sess, err := session.NewSession(s3Config)
	if err != nil {
		return nil, err
	}

	return &Client{
		svc:       s3.New(sess),
		bucket:    bucket,
		publicURL: publicURL,
	}, nil
}

// Upload uploads data to R2 and returns the public URL if configured, or the key.
func (c *Client) Upload(key string, data []byte, contentType string) (string, error) {
	_, err := c.svc.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}

    if c.publicURL != "" {
        // Simple join, ensuring slash
        base := strings.TrimRight(c.publicURL, "/")
        return fmt.Sprintf("%s/%s", base, key), nil
    }
	return key, nil
}

// List returns a list of object keys in the bucket.
func (c *Client) List() ([]string, error) {
	resp, err := c.svc.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return nil, err
	}

	var keys []string
	for _, item := range resp.Contents {
		keys = append(keys, *item.Key)
	}
	return keys, nil
}

// Delete removes an object from R2.
func (c *Client) Delete(key string) error {
	_, err := c.svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// GetURL returns the public URL for a given key.
func (c *Client) GetURL(key string) string {
    if c.publicURL == "" {
        return ""
    }
    base := strings.TrimRight(c.publicURL, "/")
    return fmt.Sprintf("%s/%s", base, key)
}

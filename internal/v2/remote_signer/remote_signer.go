// Package remotesigner offloads private key operations to S2Av2.
package remotesigner

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"google.golang.org/grpc/codes"
	"io"

	s2av2pb "github.com/google/s2a-go/internal/proto/v2/s2a_go_proto"
)

// remoteSigner implementes the crypto.Signer interface.
type remoteSigner struct {
	leafCert *x509.Certificate
	cstream  s2av2pb.S2AService_SetUpSessionClient
}

// New returns an instance of RemoteSigner, an implementation of the
// crypto.Signer interface.
func New(leafCert *x509.Certificate, cstream s2av2pb.S2AService_SetUpSessionClient) crypto.Signer {
	return &remoteSigner{leafCert, cstream}
}

func (s *remoteSigner) Public() crypto.PublicKey {
	return s.leafCert.PublicKey
}

func (s *remoteSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	signatureAlgorithm, err := getSignatureAlgorithm(opts, s.leafCert)
	if err != nil {
		return nil, err
	}

	req, err := getSignReq(signatureAlgorithm, digest)
	if err != nil {
		return nil, err
	}
	// Send request to S2Av2 to perform private key operation.
	if err := s.cstream.Send(&s2av2pb.SessionReq{
		ReqOneof: &s2av2pb.SessionReq_OffloadPrivateKeyOperationReq{
			OffloadPrivateKeyOperationReq: req,
		},
	}); err != nil {
		return nil, err
	}

	// Get the response containing config from S2Av2.
	resp, err := s.cstream.Recv()
	if err != nil {
		return nil, err
	}

	if (resp.GetStatus() != nil) && (resp.GetStatus().Code != uint32(codes.OK)) {
		return nil, fmt.Errorf("Failed to offload signing with private key to S2A: %d, %v", resp.GetStatus().Code, resp.GetStatus().Details)
	}

	return resp.GetOffloadPrivateKeyOperationResp().GetOutBytes(), nil
}

// getCert returns the leafCert field in s.
func (s *remoteSigner) getCert() *x509.Certificate {
	return s.leafCert
}

// getStream returns the cstream field in s.
func (s *remoteSigner) getStream() s2av2pb.S2AService_SetUpSessionClient {
	return s.cstream
}

func getSignReq(signatureAlgorithm s2av2pb.SignatureAlgorithm, digest []byte) (*s2av2pb.OffloadPrivateKeyOperationReq, error) {
	if (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA256) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP256R1_SHA256) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA256) {
		return &s2av2pb.OffloadPrivateKeyOperationReq{
			Operation:          s2av2pb.OffloadPrivateKeyOperationReq_SIGN,
			SignatureAlgorithm: signatureAlgorithm,
			InBytes: &s2av2pb.OffloadPrivateKeyOperationReq_Sha256Digest{
				Sha256Digest: digest,
			},
		}, nil
	} else if (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA384) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP384R1_SHA384) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA384) {
		return &s2av2pb.OffloadPrivateKeyOperationReq{
			Operation:          s2av2pb.OffloadPrivateKeyOperationReq_SIGN,
			SignatureAlgorithm: signatureAlgorithm,
			InBytes: &s2av2pb.OffloadPrivateKeyOperationReq_Sha384Digest{
				Sha384Digest: digest,
			},
		}, nil
	} else if (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA512) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP521R1_SHA512) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA512) || (signatureAlgorithm == s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ED25519) {
		return &s2av2pb.OffloadPrivateKeyOperationReq{
			Operation:          s2av2pb.OffloadPrivateKeyOperationReq_SIGN,
			SignatureAlgorithm: signatureAlgorithm,
			InBytes: &s2av2pb.OffloadPrivateKeyOperationReq_Sha512Digest{
				Sha512Digest: digest,
			},
		}, nil
	} else {
		return nil, fmt.Errorf("unknown signature algorithm: %v", signatureAlgorithm)
	}
}

// getSignatureAlgorithm returns the signature algorithm that S2A must use when
// performing a signing operation that has been offloaded by an application
// using the crypto/tls libraries.
func getSignatureAlgorithm(opts crypto.SignerOpts, leafCert *x509.Certificate) (s2av2pb.SignatureAlgorithm, error) {
	if opts == nil || leafCert == nil {
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_UNSPECIFIED, fmt.Errorf("unknown signature algorithm")
	}
	switch leafCert.PublicKeyAlgorithm {
	case x509.RSA:
		if rsaPSSOpts, ok := opts.(*rsa.PSSOptions); ok {
			return rsaPSSAlgorithm(rsaPSSOpts)
		}
		return rsaPPKCS1Algorithm(opts)
	case x509.ECDSA:
		return ecdsaAlgorithm(opts)
	case x509.Ed25519:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ED25519, nil
	default:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_UNSPECIFIED, fmt.Errorf("unknown signature algorithm: %q", leafCert.PublicKeyAlgorithm)
	}
}

func rsaPSSAlgorithm(opts *rsa.PSSOptions) (s2av2pb.SignatureAlgorithm, error) {
	switch opts.HashFunc() {
	case crypto.SHA256:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA256, nil
	case crypto.SHA384:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA384, nil
	case crypto.SHA512:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PSS_RSAE_SHA512, nil
	default:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_UNSPECIFIED, fmt.Errorf("unknown signature algorithm")
	}
}

func rsaPPKCS1Algorithm(opts crypto.SignerOpts) (s2av2pb.SignatureAlgorithm, error) {
	switch opts.HashFunc() {
	case crypto.SHA256:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA256, nil
	case crypto.SHA384:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA384, nil
	case crypto.SHA512:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_RSA_PKCS1_SHA512, nil
	default:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_UNSPECIFIED, fmt.Errorf("unknown signature algorithm")
	}
}

func ecdsaAlgorithm(opts crypto.SignerOpts) (s2av2pb.SignatureAlgorithm, error) {
	switch opts.HashFunc() {
	case crypto.SHA256:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP256R1_SHA256, nil
	case crypto.SHA384:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP384R1_SHA384, nil
	case crypto.SHA512:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_ECDSA_SECP521R1_SHA512, nil
	default:
		return s2av2pb.SignatureAlgorithm_S2A_SSL_SIGN_UNSPECIFIED, fmt.Errorf("unknown signature algorithm")
	}
}

package wire

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/qerr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Public Header", func() {
	connID := protocol.ConnectionID{0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6}

	Context("when parsing", func() {
		It("accepts a sample client header", func() {
			ver := make([]byte, 4)
			binary.BigEndian.PutUint32(ver, uint32(protocol.SupportedVersions[0]))
			flagByte := uint8(0x09)
			b := bytes.NewReader(append(append([]byte{flagByte, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6}, ver...), 0x01))
			hdr, err := parsePublicHeader(b, protocol.PerspectiveClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.VersionFlag).To(BeTrue())
			Expect(hdr.IsVersionNegotiation).To(BeFalse())
			Expect(hdr.ResetFlag).To(BeFalse())
			connID := protocol.ConnectionID{0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6}
			Expect(hdr.DestConnectionID).To(Equal(connID))
			Expect(hdr.SrcConnectionID).To(Equal(connID))
			Expect(hdr.Version).To(Equal(protocol.SupportedVersions[0]))
			Expect(hdr.SupportedVersions).To(BeEmpty())
			pn, pnLen, err := readPublicHeaderPacketNumber(b, flagByte)
			Expect(err).ToNot(HaveOccurred())
			Expect(pn).To(Equal(protocol.PacketNumber(1)))
			Expect(pnLen).To(Equal(protocol.PacketNumberLen1))
			Expect(b.Len()).To(BeZero())
		})

		It("does not accept an omittedd connection ID as a server", func() {
			b := bytes.NewReader([]byte{0x00, 0x01})
			_, err := parsePublicHeader(b, protocol.PerspectiveClient)
			Expect(err).To(MatchError(errReceivedOmittedConnectionID))
		})

		It("accepts an omitted connection ID as a client", func() {
			b := bytes.NewReader([]byte{0x00, 0x01})
			hdr, err := parsePublicHeader(b, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.OmitConnectionID).To(BeTrue())
			Expect(hdr.DestConnectionID).To(BeEmpty())
			Expect(hdr.SrcConnectionID).To(BeEmpty())
			Expect(b.Len()).To(Equal(1)) // packet number not parsed
		})

		It("rejects 0 as a connection ID", func() {
			b := bytes.NewReader([]byte{0x09, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x51, 0x30, 0x33, 0x30, 0x01})
			_, err := parsePublicHeader(b, protocol.PerspectiveClient)
			Expect(err).To(MatchError(errInvalidConnectionID))
		})

		It("parses a PUBLIC_RESET packet", func() {
			b := bytes.NewReader([]byte{0xa, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8})
			hdr, err := parsePublicHeader(b, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.ResetFlag).To(BeTrue())
			Expect(hdr.VersionFlag).To(BeFalse())
			Expect(hdr.IsVersionNegotiation).To(BeFalse())
			connID := protocol.ConnectionID{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8}
			Expect(hdr.SrcConnectionID).To(Equal(connID))
			Expect(hdr.DestConnectionID).To(Equal(connID))
		})

		It("reads a diversification nonce sent by the server", func() {
			divNonce := []byte{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
			Expect(divNonce).To(HaveLen(32))
			b := bytes.NewReader(append(append([]byte{0x0c, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c}, divNonce...), 0x37))
			hdr, err := parsePublicHeader(b, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.DestConnectionID).ToNot(BeEmpty())
			Expect(hdr.SrcConnectionID).ToNot(BeEmpty())
			Expect(hdr.DiversificationNonce).To(Equal(divNonce))
			Expect(b.Len()).To(Equal(1)) // packet number not parsed
		})

		Context("version negotiation packets", func() {
			appendVersion := func(data []byte, v protocol.VersionNumber) []byte {
				data = append(data, []byte{0, 0, 0, 0}...)
				binary.BigEndian.PutUint32(data[len(data)-4:], uint32(v))
				return data
			}

			It("parses", func() {
				connID := protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7, 8}
				versions := []protocol.VersionNumber{0x13, 0x37}
				b := bytes.NewReader(ComposeGQUICVersionNegotiation(connID, versions))
				hdr, err := parsePublicHeader(b, protocol.PerspectiveServer)
				Expect(err).ToNot(HaveOccurred())
				Expect(hdr.DestConnectionID).To(Equal(connID))
				Expect(hdr.SrcConnectionID).To(Equal(connID))
				Expect(hdr.VersionFlag).To(BeTrue())
				Expect(hdr.Version).To(BeZero()) // unitialized
				Expect(hdr.IsVersionNegotiation).To(BeTrue())
				// in addition to the versions, the supported versions might contain a reserved version number
				for _, version := range versions {
					Expect(hdr.SupportedVersions).To(ContainElement(version))
				}
				Expect(b.Len()).To(BeZero())
			})

			It("errors if it doesn't contain any versions", func() {
				b := bytes.NewReader([]byte{0x9, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c})
				_, err := parsePublicHeader(b, protocol.PerspectiveServer)
				Expect(err).To(MatchError("InvalidVersionNegotiationPacket: empty version list"))
			})

			It("reads version negotiation packets containing unsupported versions", func() {
				data := []byte{0x9, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c}
				data = appendVersion(data, 1) // unsupported version
				data = appendVersion(data, protocol.SupportedVersions[0])
				data = appendVersion(data, 99) // unsupported version
				b := bytes.NewReader(data)
				hdr, err := parsePublicHeader(b, protocol.PerspectiveServer)
				Expect(err).ToNot(HaveOccurred())
				Expect(hdr.VersionFlag).To(BeTrue())
				Expect(hdr.IsVersionNegotiation).To(BeTrue())
				Expect(hdr.SupportedVersions).To(Equal([]protocol.VersionNumber{1, protocol.SupportedVersions[0], 99}))
				Expect(b.Len()).To(BeZero())
			})

			It("errors on invalid version tags", func() {
				data := ComposeGQUICVersionNegotiation(protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7, 8}, protocol.SupportedVersions)
				data = append(data, []byte{0x13, 0x37}...)
				b := bytes.NewReader(data)
				_, err := parsePublicHeader(b, protocol.PerspectiveServer)
				Expect(err).To(MatchError(qerr.InvalidVersionNegotiationPacket))
			})
		})

		Context("Packet Number lengths", func() {
			It("accepts 1-byte packet numbers", func() {
				flagByte := uint8(0x08)
				b := bytes.NewReader([]byte{flagByte, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0xde})
				_, err := parsePublicHeader(b, protocol.PerspectiveClient)
				Expect(err).ToNot(HaveOccurred())
				pn, pnLen, err := readPublicHeaderPacketNumber(b, flagByte)
				Expect(err).ToNot(HaveOccurred())
				Expect(pn).To(Equal(protocol.PacketNumber(0xde)))
				Expect(pnLen).To(Equal(protocol.PacketNumberLen1))
				Expect(b.Len()).To(BeZero())
			})

			It("accepts 2-byte packet numbers", func() {
				flagByte := uint8(0x18)
				b := bytes.NewReader([]byte{flagByte, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0xde, 0xca})
				_, err := parsePublicHeader(b, protocol.PerspectiveClient)
				Expect(err).ToNot(HaveOccurred())
				pn, pnLen, err := readPublicHeaderPacketNumber(b, flagByte)
				Expect(err).ToNot(HaveOccurred())
				Expect(pn).To(Equal(protocol.PacketNumber(0xdeca)))
				Expect(pnLen).To(Equal(protocol.PacketNumberLen2))
				Expect(b.Len()).To(BeZero())
			})

			It("accepts 4-byte packet numbers", func() {
				flagByte := uint8(0x28)
				b := bytes.NewReader([]byte{flagByte, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0xad, 0xfb, 0xca, 0xde})
				_, err := parsePublicHeader(b, protocol.PerspectiveClient)
				Expect(err).ToNot(HaveOccurred())
				pn, pnLen, err := readPublicHeaderPacketNumber(b, flagByte)
				Expect(err).ToNot(HaveOccurred())
				Expect(pn).To(Equal(protocol.PacketNumber(0xadfbcade)))
				Expect(pnLen).To(Equal(protocol.PacketNumberLen4))
				Expect(b.Len()).To(BeZero())
			})

			It("rejects 6-byte packet numbers", func() {
				flagByte := uint8(0x38)
				b := bytes.NewReader([]byte{flagByte, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0x23, 0x42, 0xad, 0xfb, 0xca, 0xde})
				_, err := parsePublicHeader(b, protocol.PerspectiveClient)
				Expect(err).ToNot(HaveOccurred())
				_, _, err = readPublicHeaderPacketNumber(b, flagByte)
				Expect(err).To(MatchError(errInvalidPacketNumberLen))
			})
		})
	})

	Context("when writing", func() {
		It("writes a sample header as a server", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			err := hdr.writePublicHeader(b, 2, protocol.PacketNumberLen4, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.Bytes()).To(Equal([]byte{0x28, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0, 0, 0, 2}))
		})

		It("writes a sample header as a client", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			err := hdr.writePublicHeader(b, 0x1337, protocol.PacketNumberLen2, protocol.PerspectiveClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.Bytes()).To(Equal([]byte{0x18, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6, 0x13, 0x37}))
		})

		It("refuses to write a Public Header if the source and destination connection IDs are not matching", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				DestConnectionID: protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7, 8},
				SrcConnectionID:  protocol.ConnectionID{8, 7, 6, 5, 4, 3, 2, 1},
			}
			err := hdr.writePublicHeader(b, 1, protocol.PacketNumberLen1, protocol.PerspectiveClient)
			Expect(err).To(MatchError("PublicHeader: SrcConnectionID must be equal to DestConnectionID"))
		})

		It("refuses to write a Public Header if the connection ID has the wrong length", func() {
			connID := protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7}
			hdr := Header{
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			b := &bytes.Buffer{}
			err := hdr.writePublicHeader(b, 1, protocol.PacketNumberLen1, protocol.PerspectiveServer)
			Expect(err).To(MatchError("PublicHeader: wrong length for Connection ID: 7 (expected 8)"))
		})

		It("refuses to write a Public Header with a 6 byte number length", func() {
			connID := protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7, 8}
			hdr := Header{
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			b := &bytes.Buffer{}
			err := hdr.writePublicHeader(b, 1, protocol.PacketNumberLen1, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
		})

		It("omits the connection ID", func() {
			connID := protocol.ConnectionID{1, 2, 3, 4, 5, 6, 7, 8}
			b := &bytes.Buffer{}
			hdr := Header{
				DestConnectionID: connID,
				SrcConnectionID:  connID,
				OmitConnectionID: true,
			}
			err := hdr.writePublicHeader(b, 1, protocol.PacketNumberLen1, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.Bytes()).To(Equal([]byte{0x0, 0x1}))
		})

		It("writes diversification nonces", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				DestConnectionID:     connID,
				SrcConnectionID:      connID,
				DiversificationNonce: bytes.Repeat([]byte{1}, 32),
			}
			err := hdr.writePublicHeader(b, 0x42, protocol.PacketNumberLen1, protocol.PerspectiveServer)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.Bytes()).To(Equal([]byte{
				0x0c, 0x4c, 0xfa, 0x9f, 0x9b, 0x66, 0x86, 0x19, 0xf6,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
				0x42,
			}))
		})

		It("throws an error if the Reset Flagis set", func() {
			b := &bytes.Buffer{}
			hdr := Header{ResetFlag: true}
			err := hdr.writePublicHeader(b, 0x42, protocol.PacketNumberLen1, protocol.PerspectiveClient)
			Expect(err).To(MatchError("PublicHeader: Writing of Public Reset Packets not supported"))
		})

		It("doesn't write Version Negotiation Packets", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				VersionFlag:      true,
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			err := hdr.writePublicHeader(b, 0x42, protocol.PacketNumberLen1, protocol.PerspectiveServer)
			Expect(err).To(MatchError("PublicHeader: Writing of Version Negotiation Packets not supported"))
		})

		It("writes packets with Version Flag, as a client", func() {
			b := &bytes.Buffer{}
			hdr := Header{
				VersionFlag:      true,
				Version:          protocol.Version39,
				DestConnectionID: connID,
				SrcConnectionID:  connID,
			}
			err := hdr.writePublicHeader(b, 0x42, protocol.PacketNumberLen1, protocol.PerspectiveClient)
			Expect(err).ToNot(HaveOccurred())
			// must be the first assertion
			Expect(b.Len()).To(Equal(1 + 8 + 4 + 1)) // 1 FlagByte + 8 ConnectionID + 4 version number + 1 packet number
			firstByte, _ := b.ReadByte()
			Expect(firstByte & 0x01).To(Equal(uint8(1)))
			Expect(firstByte & 0x30).To(Equal(uint8(0x0)))
			Expect(string(b.Bytes()[8:12])).To(Equal("Q039"))
			Expect(b.Bytes()[12:13]).To(Equal([]byte{0x42}))
		})

		Context("getting the length", func() {
			It("gets the length of a packet sent by the client with the VersionFlag set", func() {
				hdr := Header{
					DestConnectionID: connID,
					SrcConnectionID:  connID,
					OmitConnectionID: true,
					VersionFlag:      true,
					Version:          versionBigEndian,
				}
				length := hdr.getPublicHeaderLength(protocol.PacketNumberLen2, protocol.PerspectiveClient)
				Expect(length).To(Equal(protocol.ByteCount(1 + 4 + 2))) // 1 byte public flag, 4 version number, 2 packet number
			})

			It("gets the length of a packet with longest packet number length and omitted connectionID", func() {
				hdr := Header{
					DestConnectionID: connID,
					SrcConnectionID:  connID,
					OmitConnectionID: true,
				}
				length := hdr.getPublicHeaderLength(0, protocol.PerspectiveServer)
				Expect(length).To(Equal(protocol.ByteCount(1))) // public flag
			})

			It("works with diversification nonce", func() {
				hdr := Header{
					DiversificationNonce: []byte("foo"),
				}
				length := hdr.getPublicHeaderLength(protocol.PacketNumberLen4, protocol.PerspectiveServer)
				Expect(length).To(Equal(protocol.ByteCount(1 + 8 + 3 + 4))) // 1 byte public flag, 8 byte connectionID, 3 byte DiversificationNonce, 4 packet number
			})
		})
	})

	Context("logging", func() {
		var (
			buf    *bytes.Buffer
			logger utils.Logger
		)

		BeforeEach(func() {
			buf = &bytes.Buffer{}
			logger = utils.DefaultLogger
			logger.SetLogLevel(utils.LogLevelDebug)
			log.SetOutput(buf)
		})

		AfterEach(func() {
			log.SetOutput(os.Stdout)
		})

		It("logs a Public Header containing a connection ID", func() {
			(&Header{
				DestConnectionID: protocol.ConnectionID{0x13, 0x37, 0, 0, 0xde, 0xca, 0xfb, 0xad},
				SrcConnectionID:  protocol.ConnectionID{0x13, 0x37, 0, 0, 0xde, 0xca, 0xfb, 0xad},
				Version:          protocol.Version39,
			}).logPublicHeader(logger)
			Expect(buf.String()).To(ContainSubstring("Public Header{ConnectionID: 0x13370000decafbad, Version: gQUIC 39"))
		})

		It("logs a Public Header with omitted connection ID", func() {
			(&Header{
				OmitConnectionID: true,
				Version:          protocol.Version39,
			}).logPublicHeader(logger)
			Expect(buf.String()).To(ContainSubstring("Public Header{ConnectionID: (empty)"))
		})

		It("logs a Public Header without a version", func() {
			(&Header{OmitConnectionID: true}).logPublicHeader(logger)
			Expect(buf.String()).To(ContainSubstring("Version: (unset)"))
		})

		It("logs diversification nonces", func() {
			(&Header{
				DestConnectionID:     []byte{0x13, 0x13, 0, 0, 0xde, 0xca, 0xfb, 0xad},
				SrcConnectionID:      []byte{0x13, 0x13, 0, 0, 0xde, 0xca, 0xfb, 0xad},
				DiversificationNonce: []byte{0xba, 0xdf, 0x00, 0x0d},
			}).logPublicHeader(logger)
			Expect(buf.String()).To(ContainSubstring("DiversificationNonce: []byte{0xba, 0xdf, 0x0, 0xd}"))
		})

	})
})

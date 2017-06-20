// Package rbxmesh is used to decode and encode Roblox mesh files.
package rbxmesh

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

type readReporter struct {
	r io.Reader
	n int64
}

func (r *readReporter) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func (r *readReporter) BytesRead() int64 {
	return r.n
}

// Version indicates the version of a mesh format to be used when encoding or
// decoding.
type Version int

const (
	VersionUnknown Version = -1
	Version2_00    Version = 0 // Default
	Version1_00    Version = 1
	Version1_01    Version = 2
)

// String returns a string representation of the version. It matches the
// version signature used in the mesh format. An unknown version return an
// invalid string.
func (v Version) String() string {
	switch v {
	case Version1_00:
		return "version 1.00"
	case Version1_01:
		return "version 1.01"
	case Version2_00:
		return "version 2.00"
	default:
		return "version x.xx"
	}
}

// VersionFromString matches a string to a Version, returning VersionUnknown
// if the string could not be matched. The string must be the same as the
// version signature in the mesh format, which is also the value returned by
// Version.String.
func VersionFromString(s string) Version {
	switch s {
	case Version1_00.String():
		return Version1_00
	case Version1_01.String():
		return Version1_01
	case Version2_00.String():
		return Version2_00
	}
	return VersionUnknown
}

// MeshVertex represents a single vertex in a mesh.
type MeshVertex struct {
	Position [3]float64 // Vx, Vy, Vz; The position of the vertex.
	Normal   [3]float64 // Nx, Ny, Nz; The direction of the vertex.
	Texture  [3]float64 // Tu, Tv, Tw; UV coordinates of the vertex.
	Color    [4]byte    // R, G, B, A; Not applicable if Mesh.HasColor is false.
}

// MeshFace represents a single triangle in a mesh. It consists of 3 indices,
// each pointing to a MeshVertex in a Mesh.Vertices slice.
type MeshFace [3]int

// Mesh holds the contents of a mesh file. When encoding, the data from each
// field is written. When decoding, each field is filled in according to data
// read from the format.
type Mesh struct {
	Version  Version // Version indicates the version used to decode, or the version to use when encoding.
	HasColor bool    // HasColor indicates whether each MeshVertex has color data.
	Vertices []MeshVertex
	Faces    []MeshFace
}

const nHeaderSize = 2
const nHeader = nHeaderSize + 1 + 1 + 4 + 4
const nVertex = (4 + 4 + 4) + (4 + 4 + 4) + (4 + 4 + 4)
const nColor = nVertex + (1 + 1 + 1 + 1)
const nFace = 4 + 4 + 4

// ReadFrom decodes a mesh file by reading and parsing bytes from an
// io.Reader. Fields of the Mesh are filled in according to the parsed data.
func (m *Mesh) ReadFrom(r io.Reader) (n int64, err error) {
	rr := &readReporter{r: r}
	buf := bufio.NewReader(rr)
	line, _, err := buf.ReadLine()
	if err != nil {
		return rr.BytesRead(), err
	}
	version := VersionFromString(string(line))
	m.Version = version
	switch version {
	case Version1_00, Version1_01:
		m.HasColor = false
		line, _, err := buf.ReadLine()
		if err != nil {
			return rr.BytesRead(), err
		}
		i := 0
		for ; i < len(line); i++ {
			if !('0' <= line[i] && line[i] <= '9') {
				break
			}
		}
		n, _ := strconv.Atoi(string(line[:i]))
		verts := make(map[MeshVertex]int, n)
		m.Faces = make([]MeshFace, n)
		for i := 0; i < n; i++ {
			for f := 0; f < len(m.Faces[i]); f++ {
				v := MeshVertex{}
				if _, err := fmt.Fscanf(buf, "[%f,%f,%f][%f,%f,%f][%f,%f,%f]",
					&v.Position[0], &v.Position[1], &v.Position[2],
					&v.Normal[0], &v.Normal[1], &v.Normal[2],
					&v.Texture[0], &v.Texture[1], &v.Texture[2],
				); err != nil {
					return rr.BytesRead(), err
				}
				v.Texture[1] = 1 - v.Texture[1]
				if version == Version1_00 {
					v.Position[0] *= 0.5
					v.Position[1] *= 0.5
					v.Position[2] *= 0.5
				}
				index, ok := verts[v]
				if !ok {
					index = len(verts)
					verts[v] = index
				}
				m.Faces[i][f] = index
			}
		}
		m.Vertices = make([]MeshVertex, len(verts))
		for vert, index := range verts {
			m.Vertices[index] = vert
		}
		return rr.BytesRead(), nil

	case Version2_00:
		b := make([]byte, nColor)

		// Header size
		b = b[:nHeaderSize]
		if _, err := buf.Read(b); err != nil {
			return rr.BytesRead(), err
		}
		switch int(binary.LittleEndian.Uint16(b)) {
		case nHeader:
			b = b[:nHeader]
			if _, err := buf.Read(b[nHeaderSize : nHeader-nHeaderSize]); err != nil {
				return rr.BytesRead(), err
			}
			switch int(b[2]) {
			case nVertex:
				m.HasColor = false
			case nColor:
				m.HasColor = true
			default:
				return rr.BytesRead(), errors.New("unexpected vertex size")
			}
			switch int(b[3]) {
			case nFace:
			default:
				return rr.BytesRead(), errors.New("unexpected face size")
			}
			m.Vertices = make([]MeshVertex, int(binary.LittleEndian.Uint32(b[4:8])))
			m.Faces = make([]MeshFace, int(binary.LittleEndian.Uint32(b[8:12])))

		default:
			return rr.BytesRead(), errors.New("unexpected header size")
		}

		// Vertices
		if m.HasColor {
			b = b[:nColor]
		} else {
			b = b[:nVertex]
		}
		vec := func(b []byte) [3]float64 {
			return [3]float64{
				float64(math.Float32frombits(binary.LittleEndian.Uint32(b[0:4]))),
				float64(math.Float32frombits(binary.LittleEndian.Uint32(b[4:8]))),
				float64(math.Float32frombits(binary.LittleEndian.Uint32(b[8:12]))),
			}
		}
		for i, v := range m.Vertices {
			if _, err := buf.Read(b); err != nil {
				return rr.BytesRead(), err
			}
			v.Position = vec(b[0:12])
			v.Normal = vec(b[12:24])
			v.Texture = vec(b[24:36])
			if m.HasColor {
				copy(v.Color[:], b[36:40])
			}
			m.Vertices[i] = v
		}

		// Faces
		b = b[:nFace]
		for i, f := range m.Faces {
			if _, err := buf.Read(b); err != nil {
				return rr.BytesRead(), err
			}
			f[0] = int(binary.LittleEndian.Uint32(b[0:4]))
			f[1] = int(binary.LittleEndian.Uint32(b[4:8]))
			f[2] = int(binary.LittleEndian.Uint32(b[8:12]))
			m.Faces[i] = f
		}

		return rr.BytesRead(), nil
	}
	return rr.BytesRead(), errors.New("unknown version")
}

// WriteTo encodes a mesh file by converting fields of the Mesh into a byte
// representation, which is written to an io.Writer. The format is determined
// by Mesh.Version.
func (m *Mesh) WriteTo(w io.Writer) (n int64, err error) {
	nn, err := w.Write([]byte(m.Version.String() + "\n"))
	if n += int64(nn); err != nil {
		return n, err
	}

	switch m.Version {
	case Version1_00, Version1_01:
		b := make([]byte, 0, 32)
		b = strconv.AppendUint(b, uint64(len(m.Faces)), 32)
		b = append(b, '\n')
		nn, err := w.Write(b)
		if n += int64(nn); err != nil {
			return n, err
		}
		for _, face := range m.Faces {
			for i := 0; i < len(face); i++ {
				index := face[i]
				if index < 0 || index >= len(m.Vertices) {
					return n, errors.New("index out of range")
				}
				v := m.Vertices[index]
				pos := v.Position
				if m.Version == Version1_00 {
					pos[0] *= 2
					pos[1] *= 2
					pos[2] *= 2
				}
				nn, err := fmt.Fprintf(w, "[%f,%f,%f][%f,%f,%f][%f,%f,%f]",
					float32(pos[0]), float32(pos[1]), float32(pos[2]),
					float32(v.Normal[0]), float32(v.Normal[1]), float32(v.Normal[2]),
					float32(v.Texture[0]), float32(1-v.Texture[1]), float32(v.Texture[2]),
				)
				if n += int64(nn); err != nil {
					return n, err
				}
			}
		}
		return n, nil

	case Version2_00:
		b := make([]byte, 0, nColor)
		put16 := binary.LittleEndian.PutUint16
		put32 := binary.LittleEndian.PutUint32

		// Header
		b = b[:nHeader]
		put16(b[0:2], uint16(nHeader))
		if m.HasColor {
			put16(b[2:4], uint16(nColor))
		} else {
			put16(b[2:4], uint16(nVertex))
		}
		put16(b[4:6], uint16(nFace))
		put32(b[6:10], uint32(len(m.Vertices)))
		put32(b[10:14], uint32(len(m.Faces)))
		nn, err := w.Write(b)
		if n += int64(nn); err != nil {
			return n, err
		}

		// Vertices
		putvec := func(b []byte, v [3]float64) {
			binary.LittleEndian.PutUint32(b[0:4], math.Float32bits(float32(v[0])))
			binary.LittleEndian.PutUint32(b[4:8], math.Float32bits(float32(v[1])))
			binary.LittleEndian.PutUint32(b[8:12], math.Float32bits(float32(v[2])))
		}
		if m.HasColor {
			b = b[:nColor]
		} else {
			b = b[:nVertex]
		}
		for _, vertex := range m.Vertices {
			putvec(b[0:12], vertex.Position)
			putvec(b[12:24], vertex.Normal)
			putvec(b[24:36], vertex.Texture)
			if m.HasColor {
				copy(b[36:40], vertex.Color[:])
			}
			nn, err = w.Write(b)
			if n += int64(nn); err != nil {
				return n, err
			}
		}

		// Faces
		b = b[:nFace]
		for _, face := range m.Faces {
			put32(b[0:4], uint32(face[0]))
			put32(b[4:8], uint32(face[1]))
			put32(b[8:12], uint32(face[2]))
			nn, err = w.Write(b)
			if n += int64(nn); err != nil {
				return n, err
			}
		}

		return n, nil
	}
	return 0, errors.New("unknown version")
}

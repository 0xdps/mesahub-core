import { ImageResponse } from 'next/og'

export const size = { width: 32, height: 32 }
export const contentType = 'image/png'

export default function Icon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: 32,
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: 7,
          background: '#1D6FCC',
        }}
      >
        <svg
          width="24"
          height="24"
          viewBox="0 0 40 40"
          fill="none"
        >
          {/* Mesa mark: three strata — wider at top like a table mountain */}
          <path d="M3,10 L37,10 L31,18 L9,18 Z" fill="white" />
          <path d="M9,19 L31,19 L27,26 L13,26 Z" fill="white" fillOpacity="0.65" />
          <path d="M13,27 L27,27 L25,31 L15,31 Z" fill="white" fillOpacity="0.35" />
        </svg>
      </div>
    ),
    { ...size },
  )
}

export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <path
        d="M14 0H50A14 14 0 0 1 64 14V48L48 64H14A14 14 0 0 1 0 50V14A14 14 0 0 1 14 0Z"
        fill="#fbbf24"
      />
      <path d="M48 48h16L48 64Z" fill="#d97706" />
      <text
        x="31.5"
        y="49"
        fontFamily="'Noto Sans JP','Yu Gothic','Meiryo','Hiragino Kaku Gothic ProN',sans-serif"
        fontSize="36"
        fontWeight="700"
        fill="#09090b"
        textAnchor="middle"
      >
        巻
      </text>
    </svg>
  );
}

export function Brand({ small = false }: { small?: boolean }) {
  return (
    <div className={`brand ${small ? "brand-small" : ""}`}>
      <span className="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 32 32" fill="none">
          <path
            d="m7 8 8 17M25 8l-8 17"
            stroke="currentColor"
            strokeWidth="4.3"
            strokeLinecap="round"
          />
          <circle cx="25" cy="25" r="2.5" fill="currentColor" />
        </svg>
      </span>
      <span>
        vrac<span className="brand-dot">.</span>
      </span>
    </div>
  );
}

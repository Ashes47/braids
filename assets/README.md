# Assets

| File | Used by |
| --- | --- |
| `braids-logo.png` | the README's header and the site's hero |
| `braids-icon-512.png` | the source the smaller icons are cut from, and the repo's social preview |
| `braids-icon-256.png` | `og:image`, so a link to braids.chat has a picture |
| `braids-icon-64.png` | the favicon and the site's nav |
| `frames/*.png` | the README's screenshots |

`frames/` is generated. `make frames` recaptures every screen against a fake
`~/.claude` and redraws these from the captures, so they cannot drift from what
braids actually prints. Nothing here is drawn by hand except the mark itself.

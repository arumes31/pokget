cat << 'INNER_EOF' > internal/handlers/handlers_test.go.patch
<<<<<<< SEARCH
		h.Login(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", rr.Code)
		}
	})
=======
		h.Login(rr, req)

		// The test does not send HX-Request, so it falls back to 200 with History replacement script
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
>>>>>>> REPLACE
INNER_EOF
python3 -c "
import sys
with open('internal/handlers/handlers_test.go', 'r') as f:
    text = f.read()

with open('internal/handlers/handlers_test.go.patch', 'r') as f:
    patch = f.read()

search = patch.split('<<<<<<< SEARCH\n')[1].split('=======\\n')[0]
replace = patch.split('=======\\n')[1].split('>>>>>>> REPLACE')[0]

if search in text:
    text = text.replace(search, replace)
    with open('internal/handlers/handlers_test.go', 'w') as f:
        f.write(text)
    print('Patch applied successfully')
else:
    print('Failed to apply patch')
"

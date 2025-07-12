# webexec-lite

## Go Web Server

This project includes a simple Go web server that serves static files.

### How to Run

1. Make sure you have Go installed (https://golang.org/dl/).
2. By default, the server will look for a `config.json` file in the project root. Example `config.json`:

   ```json
   {
     "homedir": "./public",
     "port": "80",
     "error_pages": {
       "404": "./public/404.html",
       "500": "./public/500.html"
     }
   }
   ```

   - `homedir`: Directory to serve static files from (default: `./public`)
   - `port`: Port to serve HTTP on (default: `80`)
   - `error_pages`: Paths to custom error pages for 404 and 500 errors (optional)

3. To run the server:

   ```sh
   go run main.go
   ```

   - You can specify a different config file with the `-config` flag:
     ```sh
     go run main.go -config=/path/to/your/config.json
     ```
   - You can override config file values with flags:
     ```sh
     go run main.go -homedir=/tmp/files -port=8080
     ```
     Flags take precedence over config file values.

4. Place your static files (e.g., `index.html`, `picture.jpg`, `file.js`) in the home directory.
5. Open your browser and go to `http://localhost:<port>` to see the server response.

### Custom Error Pages

- You can specify custom error pages for 404 (Not Found) and 500 (Internal Server Error) in `config.json` under the `error_pages` field.
- If a requested file is not found, the server will serve the specified 404 page. If the 404 page is missing, a default message is shown.
- If a server error occurs, the server will serve the specified 500 page (future support for 500 errors).
- Example error pages are provided in the `public` folder.

### Default Home Directory

- The default directory for static files is `./public`.
- An example `index.html` is provided in the `public` folder.
- You can add more files (images, JavaScript, etc.) to this directory to have them served by the web server.
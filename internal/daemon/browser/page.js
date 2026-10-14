// Follow the Reviewer: on each redraw from the review's change stream, fetch
// the stop they are at and swap it in. Moving to another stop starts it at the
// top; a redraw of the same one keeps the scroll position. A cursor event
// names the place the TUI's cursor is at: brighten what is there, dim the
// rest, and fetch nothing. dbn decides which elements are at a place, and
// marks the cursor's itself when it draws a stop.
(function () {
  var body = document.body;
  var main = document.getElementById("stop");
  var connection = document.getElementById("connection");
  var cursor = { place: "" }; // the last cursor event's
  var moves = 0; // cursor events so far

  function all(selector) {
    return Array.prototype.slice.call(main.querySelectorAll(selector));
  }

  function keyOf(root) {
    var stop = root.querySelector(".stop");
    return stop ? stop.getAttribute("data-key") : "";
  }

  function say(text) {
    connection.textContent = text;
    connection.hidden = text === "";
  }

  // reveal scrolls the cursor's elements into view when any of them is off
  // screen: centred if they fit, else from their top.
  function reveal(targets) {
    if (targets.length === 0) {
      return;
    }
    var top = targets[0].getBoundingClientRect().top;
    var bottom = targets[targets.length - 1].getBoundingClientRect().bottom;
    var height = window.innerHeight;
    if (top >= 0 && bottom <= height) {
      return;
    }
    var by = bottom - top <= height * 0.8
      ? (top + bottom) / 2 - height / 2
      : top - height * 0.1;
    window.scrollBy(0, by);
  }

  // showCursor brightens what is at a place and dims the rest. A place on
  // another stop, or none, is at nothing drawn.
  function showCursor(place) {
    var stop = main.querySelector(".stop");
    if (!stop) {
      return;
    }
    all(".cursor").forEach(function (el) {
      el.classList.remove("cursor");
    });
    var targets = place ? all('[data-place~="' + place + '"]') : [];
    targets.forEach(function (el) {
      el.classList.add("cursor");
    });
    stop.classList.toggle("following", targets.length > 0);
    reveal(targets);
  }

  // Diagrams. dbn holds each Mermaid diagram's source in a diagram container
  // (figure.diagram); Mermaid draws it in dbn's diagrams frame, which allows
  // the inline style Mermaid draws with where this page allows none, and the
  // drawing is shown here as an image, the author's own styles applying
  // inside it. Each drawing is kept by theme and source, so a redraw of the
  // stop shows it again without Mermaid drawing it again.
  var dark = window.matchMedia("(prefers-color-scheme: dark)");
  var drawer = null; // a promise of the diagrams frame's window, once Mermaid has loaded in it
  var drawings = {}; // promises of drawings, by theme and source
  var drawingQueue = Promise.resolve(); // Mermaid draws one at a time
  var mermaidTheme = ""; // the theme Mermaid was last set up with
  var drawn = 0; // diagrams Mermaid has drawn, which name the next
  // Mermaid draws text at 16px. A wide diagram is shrunk to fit the page down
  // to text of 10px, and scrolls sideways past that.
  var legible = 10 / 16;

  // loadDrawer makes the diagrams frame the first time a stop shows a
  // diagram, and gives its window once Mermaid has loaded in it.
  function loadDrawer() {
    if (drawer !== null) {
      return drawer;
    }
    drawer = new Promise(function (resolve, reject) {
      var frame = document.createElement("iframe");
      frame.className = "diagrams-frame";
      frame.setAttribute("aria-hidden", "true");
      frame.tabIndex = -1;
      frame.addEventListener("load", function () {
        if (frame.contentWindow.mermaid === undefined) {
          reject(new Error("dbn could not load Mermaid to draw this diagram."));
          return;
        }
        resolve(frame.contentWindow);
      });
      frame.src = body.getAttribute("data-diagrams-url");
      body.appendChild(frame);
    });
    // A frame that failed to load is made again for the next diagram.
    drawer.catch(function () {
      drawer = null;
    });
    return drawer;
  }

  // drawWithMermaid has Mermaid draw a diagram's source in a theme, as an SVG.
  function drawWithMermaid(theme, source) {
    var drawing = drawingQueue
      .then(loadDrawer)
      .then(function (frame) {
        if (mermaidTheme !== theme) {
          frame.mermaid.initialize({
            startOnLoad: false,
            securityLevel: "strict",
            suppressErrorRendering: true,
            theme: theme,
          });
          mermaidTheme = theme;
        }
        drawn++;
        return frame.mermaid.render("dbn-diagram-" + drawn, source).then(function (result) {
          return pictureOf(frame, result.svg);
        });
      });
    drawingQueue = drawing.catch(function () {});
    return drawing;
  }

  // pictureOf makes an image of Mermaid's SVG, at the size Mermaid drew it.
  // The SVG is read in the diagrams frame, whose policy allows the inline
  // style it holds: read here, each style in it would be reported as refused.
  function pictureOf(frame, svg) {
    var root = new frame.DOMParser().parseFromString(svg, "image/svg+xml").documentElement;
    if (root.localName !== "svg" || root.getElementsByTagName("parsererror").length > 0) {
      throw new Error("Mermaid drew something dbn could not show.");
    }
    var box = root.viewBox.baseVal;
    var width = Math.ceil(box.width);
    var height = Math.ceil(box.height);
    // Mermaid sizes its SVG to the page it is drawn in; as an image it is
    // sized here, by the page's style.
    root.setAttribute("width", String(width));
    root.setAttribute("height", String(height));
    root.removeAttribute("style");
    var blob = new Blob([new frame.XMLSerializer().serializeToString(root)], { type: "image/svg+xml" });
    return { url: URL.createObjectURL(blob), width: width, height: height };
  }

  // drawingOf is a diagram's drawing in a theme, or what stopped Mermaid
  // drawing it. A diagram that cannot be drawn is not tried again; a frame
  // that failed to load is.
  function drawingOf(theme, source) {
    var key = theme + "\n" + source;
    if (drawings[key] === undefined) {
      drawings[key] = drawWithMermaid(theme, source).catch(function (error) {
        if (drawer === null) {
          delete drawings[key];
        }
        return { error: messageOf(error) };
      });
    }
    return drawings[key];
  }

  function messageOf(error) {
    if (error instanceof Error) {
      return error.message;
    }
    return String(error);
  }

  // showDrawing puts a drawing in its diagram container, in place of the
  // source; or what stopped Mermaid drawing it, above the source.
  function showDrawing(figure, drawing) {
    var picture = figure.querySelector(".diagram-picture");
    if (picture === null) {
      picture = document.createElement("div");
      picture.className = "diagram-picture";
      // Below the container's label, if it has one.
      figure.insertBefore(picture, figure.querySelector(".diagram-source"));
    }
    picture.textContent = "";
    var failed = drawing.error !== undefined;
    figure.classList.toggle("drawn", !failed);
    figure.classList.toggle("failed", failed);
    if (failed) {
      var problem = document.createElement("div");
      problem.className = "diagram-error";
      var heading = document.createElement("p");
      heading.className = "diagram-error-heading";
      heading.textContent = "Mermaid could not draw this diagram";
      var message = document.createElement("pre");
      message.textContent = drawing.error;
      problem.appendChild(heading);
      problem.appendChild(message);
      picture.appendChild(problem);
      return;
    }
    var image = document.createElement("img");
    image.src = drawing.url;
    image.width = drawing.width;
    image.height = drawing.height;
    image.alt = "Mermaid diagram";
    image.style.minWidth = Math.min(drawing.width, Math.round(drawing.width * legible)) + "px";
    picture.appendChild(image);
  }

  // arrangePair lays a changed diagram's "before" and "after" side by side
  // when both drawings fit across the page at full size, and one above the
  // other otherwise, where each can shrink to the page's width.
  function arrangePair(pair) {
    var images = pair.querySelectorAll(".diagram-picture img");
    var fits = false;
    if (pair.querySelectorAll("figure.diagram").length === 2 && images.length === 2) {
      var gap = 24; // page.css's gap between the two
      fits = Number(images[0].getAttribute("width")) + Number(images[1].getAttribute("width")) + gap <= pair.clientWidth;
    }
    pair.classList.toggle("side-by-side", fits);
  }

  // drawDiagrams draws each diagram on the stop in the browser's theme, and
  // settles once every one is shown.
  function drawDiagrams() {
    var theme = dark.matches ? "dark" : "default";
    return Promise.all(all("figure.diagram").map(function (figure) {
      var source = figure.querySelector(".diagram-source").textContent;
      return drawingOf(theme, source).then(function (drawing) {
        showDrawing(figure, drawing);
        var pair = figure.closest(".diagram-pair");
        if (pair !== null) {
          arrangePair(pair);
        }
      });
    }));
  }

  function redraw() {
    var seen = moves;
    fetch(body.getAttribute("data-stop-url"), { cache: "no-store" })
      .then(function (response) {
        if (response.status === 404) {
          throw new Error("dbn is no longer holding this review.");
        }
        if (!response.ok) {
          throw new Error("dbn answered " + response.status + ".");
        }
        return response.text();
      })
      .then(function (html) {
        var was = keyOf(main);
        main.innerHTML = html;
        if (keyOf(main) !== was) {
          window.scrollTo(0, 0);
        }
        say("");
        // The stop was drawn with the cursor marked as it was then, which a
        // move reported while it was on its way may have overtaken.
        if (moves !== seen) {
          showCursor(cursor.place);
        } else {
          reveal(all(".cursor"));
        }
        return drawDiagrams();
      })
      .then(function () {
        // A drawing takes the place of its source at another height, which
        // may have moved the cursor's elements (a diagram among them) away.
        reveal(all(".cursor"));
      })
      .catch(function (error) {
        say(error.message);
      });
  }

  drawDiagrams();
  dark.addEventListener("change", drawDiagrams);
  window.addEventListener("resize", function () {
    all(".diagram-pair").forEach(arrangePair);
  });

  var events = new EventSource(body.getAttribute("data-events-url"));
  events.addEventListener("redraw", redraw);
  events.addEventListener("cursor", function (event) {
    cursor = JSON.parse(event.data);
    moves++;
    showCursor(cursor.place);
  });
  // Opening again after losing dbn may have missed moves, so read what is
  // current.
  events.addEventListener("open", redraw);
  events.addEventListener("error", function () {
    say("Lost dbn. Reconnecting…");
  });
})();

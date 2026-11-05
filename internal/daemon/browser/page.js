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
  // screen or under the Step's bar: centred in the room below the bar if they
  // fit, else from their top. On the stop's first place it scrolls to the top
  // of the page instead, so the Step's header shows whole above it, so long as
  // they are still on screen from there.
  function reveal(targets) {
    if (targets.length === 0) {
      return;
    }
    var top = targets[0].getBoundingClientRect().top;
    var bottom = targets[targets.length - 1].getBoundingClientRect().bottom;
    var height = window.innerHeight;
    if (targets[0] === main.querySelector("[data-place]") && bottom + window.scrollY <= height) {
      window.scrollTo(0, 0);
      return;
    }
    var bar = main.querySelector(".progress");
    var above = bar !== null ? Math.max(bar.getBoundingClientRect().bottom, 0) : 0;
    if (top >= above && bottom <= height) {
      return;
    }
    var room = height - above;
    var by = bottom - top <= room * 0.8
      ? (top + bottom) / 2 - (above + room / 2)
      : top - above - room * 0.1;
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
          return drawingFrom(frame, result.svg);
        });
      });
    drawingQueue = drawing.catch(function () {});
    return drawing;
  }

  // drawingFrom makes the drawing the page shows from Mermaid's SVG: an image
  // of it, at the size Mermaid drew it. The SVG is read in the diagrams frame,
  // whose policy allows the inline style it holds: read here, each style in it
  // would be reported as refused. Mermaid writes its labels as HTML, a line
  // break as an unclosed <br>, so the SVG is read as HTML; written out again,
  // it is the well-formed XML an image needs.
  function drawingFrom(frame, svg) {
    var root = new frame.DOMParser().parseFromString(svg, "text/html").body.firstElementChild;
    if (root === null || root.localName !== "svg") {
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
    var holder = figure.querySelector(".diagram-drawing");
    if (holder === null) {
      holder = document.createElement("div");
      holder.className = "diagram-drawing";
      // Below the container's label, if it has one.
      figure.insertBefore(holder, figure.querySelector(".diagram-source"));
    }
    holder.textContent = "";
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
      holder.appendChild(problem);
      return;
    }
    var image = document.createElement("img");
    image.src = drawing.url;
    image.width = drawing.width;
    image.height = drawing.height;
    image.alt = "Mermaid diagram";
    image.style.minWidth = Math.min(drawing.width, Math.round(drawing.width * legible)) + "px";
    holder.appendChild(image);
  }

  // gapOf is the gap page.css sets between a changed diagram's two drawings
  // side by side.
  function gapOf(pair) {
    var gap = parseFloat(window.getComputedStyle(pair).getPropertyValue("--diagram-gap"));
    return Number.isNaN(gap) ? 0 : gap;
  }

  // widthsOf are the full-size widths of the drawings shown in an element.
  function widthsOf(element) {
    return Array.prototype.map.call(element.querySelectorAll(".diagram-drawing img"), function (image) {
      return Number(image.getAttribute("width"));
    });
  }

  // arrangePair lays a changed diagram's "before" and "after" side by side
  // when both drawings fit across the page at full size, and one above the
  // other otherwise, where each can shrink to the page's width.
  function arrangePair(pair) {
    var widths = widthsOf(pair);
    var fits = pair.querySelectorAll("figure.diagram").length === 2 && widths.length === 2 &&
      widths[0] + widths[1] + gapOf(pair) <= pair.clientWidth;
    pair.classList.toggle("side-by-side", fits);
  }

  // blockOf is the block a diagram is drawn in: the diagram itself, or the
  // changed diagram it is the "before" or "after" of.
  function blockOf(figure) {
    var change = figure.closest(".diagram-change");
    return change !== null ? change : figure;
  }

  // widen lets a diagram's block reach past the page's column, toward the
  // window's right edge, when its drawings are wider than the column, so a
  // wide window shows them at full size, or nearer it, rather than shrunk or
  // scrolling sideways. It never makes the block narrower than page.css does.
  function widen(block) {
    block.style.width = "";
    var widths = widthsOf(block);
    if (widths.length === 0) {
      return;
    }
    var box = block.getBoundingClientRect();
    var style = window.getComputedStyle(block);
    var padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
    var gutter = parseFloat(window.getComputedStyle(main).paddingRight);
    var room = document.documentElement.clientWidth - gutter - box.left;
    var wanted = Math.max.apply(null, widths) + padding;
    var pair = block.querySelector(".diagram-pair");
    if (pair !== null && widths.length === 2) {
      var beside = widths[0] + widths[1] + gapOf(pair) + padding;
      if (beside <= room) {
        wanted = beside;
      }
    }
    if (wanted > box.width) {
      block.style.width = Math.min(wanted, Math.max(room, box.width)) + "px";
    }
  }

  // layOut fits each diagram on the stop to the window: its block widened,
  // then a changed one's pair arranged in the width that gives it.
  function layOut() {
    all("figure.diagram").map(blockOf).forEach(widen);
    all(".diagram-pair").forEach(arrangePair);
  }

  // ownText is the part of a block that is its own: for a list item, what
  // comes before its note and any list nested in it; for any other block, all
  // of it.
  function ownText(block) {
    var range = document.createRange();
    range.selectNodeContents(block);
    if (block.localName !== "li") {
      return range;
    }
    for (var child = block.firstElementChild; child !== null; child = child.nextElementSibling) {
      if (child.localName === "ul" || child.localName === "ol" || child.classList.contains("comment")) {
        range.setEndBefore(child);
        break;
      }
    }
    return range;
  }

  // markComments draws the bar beside each block of a Document a Comment is
  // on, in the Document's margin, as tall as the block's own text.
  function markComments() {
    all(".document .comment-bar").forEach(function (bar) {
      bar.remove();
    });
    all(".document").forEach(function (doc) {
      var origin = doc.getBoundingClientRect();
      Array.prototype.forEach.call(doc.querySelectorAll(".commented"), function (block) {
        var text = ownText(block).getBoundingClientRect();
        if (text.height === 0) {
          return;
        }
        var bar = document.createElement("div");
        bar.className = "comment-bar";
        bar.style.top = text.top - origin.top + "px";
        bar.style.height = text.height + "px";
        doc.appendChild(bar);
      });
    });
  }

  // drawDiagrams draws each diagram on the stop in the browser's theme, and
  // settles once every one is shown.
  function drawDiagrams() {
    var theme = dark.matches ? "dark" : "default";
    return Promise.all(all("figure.diagram").map(function (figure) {
      var source = figure.querySelector(".diagram-source").textContent;
      return drawingOf(theme, source).then(function (drawing) {
        showDrawing(figure, drawing);
        widen(blockOf(figure));
        var pair = figure.closest(".diagram-pair");
        if (pair !== null) {
          arrangePair(pair);
        }
      });
    }));
  }

  // Whether long lines of code wrap is the Reviewer's choice for this
  // browser, not the review's, so it is kept here and outlives a redraw.
  // Storage can be refused; the page then starts unwrapped every time.
  var wrapKey = "dbn.wrapLines";
  var wrapping = false;
  try {
    wrapping = window.localStorage.getItem(wrapKey) === "on";
  } catch (error) {}

  // showWrap draws the stop as the choice says: wrapped or not, its switch
  // set to match, and each code line's indent measured so a wrapped line
  // hangs under its own code.
  function showWrap() {
    body.classList.toggle("wrap-lines", wrapping);
    all("[data-wrap-lines]").forEach(function (box) {
      box.checked = wrapping;
    });
    all(".row .text").forEach(function (text) {
      var lead = /^[ \t]*/.exec(text.textContent)[0];
      var columns = 0;
      for (var i = 0; i < lead.length; i++) {
        // A tab reaches the next of the browser's tab stops, every 8 columns.
        columns = lead[i] === "\t" ? columns + 8 - (columns % 8) : columns + 1;
      }
      text.style.setProperty("--hang", columns + "ch");
    });
  }

  main.addEventListener("change", function (event) {
    if (!event.target.matches("[data-wrap-lines]")) {
      return;
    }
    wrapping = event.target.checked;
    try {
      window.localStorage.setItem(wrapKey, wrapping ? "on" : "off");
    } catch (error) {}
    showWrap();
  });

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
        showWrap();
        markComments();
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

  showWrap();
  drawDiagrams();
  dark.addEventListener("change", drawDiagrams);
  window.addEventListener("resize", layOut);
  // A Comment's bar is placed by where its block is drawn, which moves
  // whenever what is above it changes size: a drawing taking the place of
  // its source, an image loading, the window being resized.
  new ResizeObserver(markComments).observe(main);

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

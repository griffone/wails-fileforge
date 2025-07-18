// @ts-check
// Cynhyrchwyd y ffeil hon yn awtomatig. PEIDIWCH Â MODIWL
// This file is automatically generated. DO NOT EDIT

// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore: Unused imports
import { Create as $Create } from "@wailsio/runtime";

/**
 * New models for batch conversion
 */
export class BatchConversionRequest {
    /**
     * Creates a new BatchConversionRequest instance.
     * @param {Partial<BatchConversionRequest>} [$$source = {}] - The source object to create the BatchConversionRequest.
     */
    constructor($$source = {}) {
        if (!("inputPaths" in $$source)) {
            /**
             * @member
             * @type {string[]}
             */
            this["inputPaths"] = [];
        }
        if (!("outputDir" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["outputDir"] = "";
        }
        if (!("format" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["format"] = "";
        }
        if (!("workers" in $$source)) {
            /**
             * Number of concurrent workers
             * @member
             * @type {number}
             */
            this["workers"] = 0;
        }
        if (!("options" in $$source)) {
            /**
             * @member
             * @type {{ [_: string]: any }}
             */
            this["options"] = {};
        }
        if (!("category" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["category"] = "";
        }
        if (!("keepStructure" in $$source)) {
            /**
             * Whether to maintain directory structure
             * @member
             * @type {boolean}
             */
            this["keepStructure"] = false;
        }

        Object.assign(this, $$source);
    }

    /**
     * Creates a new BatchConversionRequest instance from a string or object.
     * @param {any} [$$source = {}]
     * @returns {BatchConversionRequest}
     */
    static createFrom($$source = {}) {
        const $$createField0_0 = $$createType0;
        const $$createField4_0 = $$createType1;
        let $$parsedSource = typeof $$source === 'string' ? JSON.parse($$source) : $$source;
        if ("inputPaths" in $$parsedSource) {
            $$parsedSource["inputPaths"] = $$createField0_0($$parsedSource["inputPaths"]);
        }
        if ("options" in $$parsedSource) {
            $$parsedSource["options"] = $$createField4_0($$parsedSource["options"]);
        }
        return new BatchConversionRequest(/** @type {Partial<BatchConversionRequest>} */($$parsedSource));
    }
}

export class BatchConversionResult {
    /**
     * Creates a new BatchConversionResult instance.
     * @param {Partial<BatchConversionResult>} [$$source = {}] - The source object to create the BatchConversionResult.
     */
    constructor($$source = {}) {
        if (!("success" in $$source)) {
            /**
             * @member
             * @type {boolean}
             */
            this["success"] = false;
        }
        if (!("message" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["message"] = "";
        }
        if (!("totalFiles" in $$source)) {
            /**
             * @member
             * @type {number}
             */
            this["totalFiles"] = 0;
        }
        if (!("successCount" in $$source)) {
            /**
             * @member
             * @type {number}
             */
            this["successCount"] = 0;
        }
        if (!("failureCount" in $$source)) {
            /**
             * @member
             * @type {number}
             */
            this["failureCount"] = 0;
        }
        if (!("results" in $$source)) {
            /**
             * @member
             * @type {ConversionResult[]}
             */
            this["results"] = [];
        }
        if (!("error" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["error"] = "";
        }

        Object.assign(this, $$source);
    }

    /**
     * Creates a new BatchConversionResult instance from a string or object.
     * @param {any} [$$source = {}]
     * @returns {BatchConversionResult}
     */
    static createFrom($$source = {}) {
        const $$createField5_0 = $$createType3;
        let $$parsedSource = typeof $$source === 'string' ? JSON.parse($$source) : $$source;
        if ("results" in $$parsedSource) {
            $$parsedSource["results"] = $$createField5_0($$parsedSource["results"]);
        }
        return new BatchConversionResult(/** @type {Partial<BatchConversionResult>} */($$parsedSource));
    }
}

export class ConversionRequest {
    /**
     * Creates a new ConversionRequest instance.
     * @param {Partial<ConversionRequest>} [$$source = {}] - The source object to create the ConversionRequest.
     */
    constructor($$source = {}) {
        if (!("inputPath" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["inputPath"] = "";
        }
        if (!("outputPath" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["outputPath"] = "";
        }
        if (!("format" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["format"] = "";
        }
        if (!("options" in $$source)) {
            /**
             * @member
             * @type {{ [_: string]: any }}
             */
            this["options"] = {};
        }
        if (!("category" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["category"] = "";
        }

        Object.assign(this, $$source);
    }

    /**
     * Creates a new ConversionRequest instance from a string or object.
     * @param {any} [$$source = {}]
     * @returns {ConversionRequest}
     */
    static createFrom($$source = {}) {
        const $$createField3_0 = $$createType1;
        let $$parsedSource = typeof $$source === 'string' ? JSON.parse($$source) : $$source;
        if ("options" in $$parsedSource) {
            $$parsedSource["options"] = $$createField3_0($$parsedSource["options"]);
        }
        return new ConversionRequest(/** @type {Partial<ConversionRequest>} */($$parsedSource));
    }
}

export class ConversionResult {
    /**
     * Creates a new ConversionResult instance.
     * @param {Partial<ConversionResult>} [$$source = {}] - The source object to create the ConversionResult.
     */
    constructor($$source = {}) {
        if (!("inputPath" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["inputPath"] = "";
        }
        if (!("outputPath" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["outputPath"] = "";
        }
        if (!("success" in $$source)) {
            /**
             * @member
             * @type {boolean}
             */
            this["success"] = false;
        }
        if (!("message" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["message"] = "";
        }
        if (!("error" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["error"] = "";
        }

        Object.assign(this, $$source);
    }

    /**
     * Creates a new ConversionResult instance from a string or object.
     * @param {any} [$$source = {}]
     * @returns {ConversionResult}
     */
    static createFrom($$source = {}) {
        let $$parsedSource = typeof $$source === 'string' ? JSON.parse($$source) : $$source;
        return new ConversionResult(/** @type {Partial<ConversionResult>} */($$parsedSource));
    }
}

export class SupportedFormat {
    /**
     * Creates a new SupportedFormat instance.
     * @param {Partial<SupportedFormat>} [$$source = {}] - The source object to create the SupportedFormat.
     */
    constructor($$source = {}) {
        if (!("category" in $$source)) {
            /**
             * @member
             * @type {string}
             */
            this["category"] = "";
        }
        if (!("formats" in $$source)) {
            /**
             * @member
             * @type {string[]}
             */
            this["formats"] = [];
        }

        Object.assign(this, $$source);
    }

    /**
     * Creates a new SupportedFormat instance from a string or object.
     * @param {any} [$$source = {}]
     * @returns {SupportedFormat}
     */
    static createFrom($$source = {}) {
        const $$createField1_0 = $$createType0;
        let $$parsedSource = typeof $$source === 'string' ? JSON.parse($$source) : $$source;
        if ("formats" in $$parsedSource) {
            $$parsedSource["formats"] = $$createField1_0($$parsedSource["formats"]);
        }
        return new SupportedFormat(/** @type {Partial<SupportedFormat>} */($$parsedSource));
    }
}

// Private type creation functions
const $$createType0 = $Create.Array($Create.Any);
const $$createType1 = $Create.Map($Create.Any, $Create.Any);
const $$createType2 = ConversionResult.createFrom;
const $$createType3 = $Create.Array($$createType2);
